-- 000026_model_usage_reservation_expiry：释放进程崩溃或超时留下的配额预留。

ALTER TABLE project_model_usage_reservations
    ADD COLUMN expires_at TIMESTAMPTZ NOT NULL DEFAULT (now() + interval '1 hour');

CREATE INDEX project_model_usage_expiry
    ON project_model_usage_reservations (expires_at)
    WHERE status = 'reserved';
