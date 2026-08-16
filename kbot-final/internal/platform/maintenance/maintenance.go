// Package maintenance 维护审计与调用日志分区，并归档过期分区。
package maintenance

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Q1mi/kbot/internal/infrastructure/objstore"
	"github.com/Q1mi/kbot/internal/infrastructure/postgres"
)

// 异步任务类型(worker cron 调度)。
const (
	TaskEnsurePartitions  = "maintenance:ensure-partitions"
	TaskArchivePartitions = "maintenance:archive-partitions"
)

// partitionedTables 是按月分区、需运维的表(均 PARTITION BY RANGE created_at)。
var partitionedTables = []string{"audit_logs", "model_call_logs"}

// ErrArchiveUnavailable 表示归档对象存储未配置或初始化失败。
var ErrArchiveUnavailable = errors.New("archive object storage unavailable")

// Service 持有数据库与归档对象存储。archive 为空时归档会安全失败并保留原分区。
type Service struct {
	db          *pgxpool.Pool
	archive     *objstore.Client // bucket = kbot-archive
	afterMonths int
}

func NewService(db *pgxpool.Pool, archive *objstore.Client, afterMonths int) *Service {
	if afterMonths <= 0 {
		afterMonths = 13
	}
	return &Service{db: db, archive: archive, afterMonths: afterMonths}
}

// EnsureUpcomingPartitions 为每张分区表确保上月/本月/下月分区存在。
func (s *Service) EnsureUpcomingPartitions(ctx context.Context, now time.Time) error {
	for _, t := range partitionedTables {
		if err := postgres.EnsurePartitionsAround(ctx, s.db, t, now); err != nil {
			return err
		}
	}
	log.Printf("[maintenance] ensured monthly partitions around %s for %v", now.Format("2006-01"), partitionedTables)
	return nil
}

// ArchiveOldPartitions 把月龄超过 afterMonths 的分区导出到 MinIO(kbot-archive/{prefix}/{YYYY-MM}.csv.gz)后 detach+drop。
// 返回归档的分区数。
func (s *Service) ArchiveOldPartitions(ctx context.Context, now time.Time) (int, error) {
	if s.archive == nil {
		return 0, ErrArchiveUnavailable
	}
	cutoff := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -s.afterMonths, 0)
	archived := 0
	for _, table := range partitionedTables {
		parts, err := postgres.ListMonthlyPartitions(ctx, s.db, table)
		if err != nil {
			return archived, err
		}
		for _, p := range parts {
			if p.Month.After(cutoff) {
				continue // 还在留证窗口内(cutoff 月及更早才归档:即 afterMonths 个月前的分区)
			}
			data, err := postgres.CopyPartitionGzip(ctx, s.db, p.Name)
			if err != nil {
				return archived, err
			}
			key := fmt.Sprintf("%s/%04d-%02d.csv.gz", archivePrefix(table), p.Month.Year(), int(p.Month.Month()))
			if err := s.archive.Put(ctx, key, bytes.NewReader(data), int64(len(data)), "application/gzip"); err != nil {
				return archived, err
			}
			log.Printf("[maintenance] archived %s → %s/%s (%d bytes gz)", p.Name, s.archive.Bucket(), key, len(data))
			if err := postgres.DetachAndDrop(ctx, s.db, table, p.Name); err != nil {
				return archived, err
			}
			archived++
		}
	}
	log.Printf("[maintenance] archive done: %d partitions older than %s", archived, cutoff.Format("2006-01"))
	return archived, nil
}

// archivePrefix 把 audit_logs→audit、model_call_logs→model_call 作为归档目录名。
func archivePrefix(table string) string { return strings.TrimSuffix(table, "_logs") }
