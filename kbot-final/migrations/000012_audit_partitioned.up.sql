-- 000012_audit_partitioned：审计日志月分区。
-- append-only;按月分区;默认留存 1 年(冷分区 6 个月迁 MinIO,见 ADR 0007)。
-- default 分区承接超出预创建范围的数据；worker 负责创建新分区与归档旧分区。

-- before_json/after_json/ip/ua 对齐 domain.AuditLog 的 *string(可空 TEXT;ip 用 TEXT 不用 INET 以兼容任意字符串)。
CREATE TABLE audit_logs (
    id            BIGSERIAL,
    actor         TEXT NOT NULL DEFAULT '',
    action        TEXT NOT NULL DEFAULT '',
    resource_type TEXT NOT NULL DEFAULT '',
    resource_id   TEXT NOT NULL DEFAULT '',
    before_json   TEXT,
    after_json    TEXT,
    ip            TEXT,
    ua            TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

CREATE TABLE audit_logs_default PARTITION OF audit_logs DEFAULT;

-- 父表索引自动下放到每个分区
CREATE INDEX audit_logs_actor      ON audit_logs (actor, created_at DESC);
CREATE INDEX audit_logs_resource   ON audit_logs (resource_type, resource_id, created_at DESC);
CREATE INDEX audit_logs_created_at ON audit_logs (created_at DESC);
