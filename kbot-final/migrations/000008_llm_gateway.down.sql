DROP TABLE IF EXISTS usage_metrics;
DROP TABLE IF EXISTS model_call_logs;   -- 级联 DROP 其分区(含 model_call_logs_default)
DROP TABLE IF EXISTS routing_policies;
DROP TABLE IF EXISTS model_aliases;
DROP TABLE IF EXISTS providers;
