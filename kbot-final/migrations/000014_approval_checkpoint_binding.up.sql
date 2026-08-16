-- 000014_approval_checkpoint_binding:checkpoint 与触发它的审批一一绑定。
--
-- 历史 checkpoint 没有 approval_id，保留为 NULL 以保证升级兼容；新运行路径始终写入该字段。
ALTER TABLE checkpoints ADD COLUMN approval_id UUID;

ALTER TABLE checkpoints
    ADD CONSTRAINT checkpoints_approval_fk
    FOREIGN KEY (approval_id) REFERENCES approvals(id) ON DELETE CASCADE;

CREATE UNIQUE INDEX checkpoints_approval
    ON checkpoints (approval_id)
    WHERE approval_id IS NOT NULL;
