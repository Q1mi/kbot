package postgres

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// 月分区命名:{table}_{YYYY}_{MM}。
func monthlyPartitionName(table string, t time.Time) string {
	t = t.UTC()
	return fmt.Sprintf("%s_%04d_%02d", table, t.Year(), int(t.Month()))
}

// EnsureMonthlyPartition 确保 t 所在月份 [月初, 次月初) 的分区存在(幂等)。
func EnsureMonthlyPartition(ctx context.Context, db *pgxpool.Pool, table string, t time.Time) error {
	start := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	name := monthlyPartitionName(table, start)
	_, err := db.Exec(ctx, fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF %s FOR VALUES FROM ('%s') TO ('%s')`,
		name, table, start.Format("2006-01-02"), end.Format("2006-01-02")))
	if err != nil {
		return fmt.Errorf("ensure partition %s: %w", name, err)
	}
	return nil
}

// EnsurePartitionsAround 确保上月/本月/下月分区都在(写入即落到真实月分区,而非 default 兜底)。
func EnsurePartitionsAround(ctx context.Context, db *pgxpool.Pool, table string, now time.Time) error {
	for _, off := range []int{-1, 0, 1} {
		if err := EnsureMonthlyPartition(ctx, db, table, now.AddDate(0, off, 0)); err != nil {
			return err
		}
	}
	return nil
}

// MonthlyPartition 描述一个已存在的月分区。
type MonthlyPartition struct {
	Name  string
	Month time.Time // 该分区对应月份的月初(UTC)
}

var partitionRe = regexp.MustCompile(`_(\d{4})_(\d{2})$`)

// ListMonthlyPartitions 列出 table 的所有 {table}_{YYYY}_{MM} 月分区(不含 *_default)。
func ListMonthlyPartitions(ctx context.Context, db *pgxpool.Pool, table string) ([]MonthlyPartition, error) {
	rows, err := db.Query(ctx, `
		SELECT c.relname
		FROM pg_inherits i
		JOIN pg_class c ON c.oid = i.inhrelid
		JOIN pg_class p ON p.oid = i.inhparent
		WHERE p.relname = $1 AND c.relname ~ ('^' || $1 || '_[0-9]{4}_[0-9]{2}$')
		ORDER BY c.relname`, table)
	if err != nil {
		return nil, fmt.Errorf("list partitions: %w", err)
	}
	defer rows.Close()
	var out []MonthlyPartition
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		m := partitionRe.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		var y, mo int
		fmt.Sscanf(m[1]+" "+m[2], "%d %d", &y, &mo)
		out = append(out, MonthlyPartition{Name: name, Month: time.Date(y, time.Month(mo), 1, 0, 0, 0, 0, time.UTC)})
	}
	return out, rows.Err()
}

// CopyPartitionGzip 把整个分区表 COPY 成 gzip 压缩的 CSV(带表头),返回字节。课程数据量小,内存缓冲即可。
func CopyPartitionGzip(ctx context.Context, db *pgxpool.Pool, partition string) ([]byte, error) {
	conn, err := db.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire: %w", err)
	}
	defer conn.Release()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	sql := fmt.Sprintf(`COPY %s TO STDOUT WITH (FORMAT csv, HEADER)`, partition)
	if _, err := conn.Conn().PgConn().CopyTo(ctx, gz, sql); err != nil {
		return nil, fmt.Errorf("copy %s: %w", partition, err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("gzip close: %w", err)
	}
	return buf.Bytes(), nil
}

// DetachAndDrop 把分区从父表摘下并删除(归档成功后调用)。
func DetachAndDrop(ctx context.Context, db *pgxpool.Pool, parent, partition string) error {
	if _, err := db.Exec(ctx, fmt.Sprintf(`ALTER TABLE %s DETACH PARTITION %s`, parent, partition)); err != nil {
		return fmt.Errorf("detach %s: %w", partition, err)
	}
	if _, err := db.Exec(ctx, fmt.Sprintf(`DROP TABLE %s`, partition)); err != nil {
		return fmt.Errorf("drop %s: %w", partition, err)
	}
	return nil
}
