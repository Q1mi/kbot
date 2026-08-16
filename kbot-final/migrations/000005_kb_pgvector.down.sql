DROP TABLE IF EXISTS connector_instances;
DROP TABLE IF EXISTS kb_ingest_jobs;
DROP TABLE IF EXISTS kb_chunks;
DROP TABLE IF EXISTS kb_documents;
DROP TABLE IF EXISTS kbs;
-- 扩展 vector 故意保留(其他库/迁移可能依赖);如需彻底清理手动 DROP EXTENSION vector。
