# 跨境电商运营与供应链协同 Agent

该目录是独立 Go Module，提供跨境电商业务模拟系统、Tool 契约、Skill、知识库和 Eval 样本。它通过 HTTP Tool 接入 kbot，不导入 kbot 或保险项目的 Go 包。

## 本地运行

```bash
make test
make run
curl http://localhost:8091/healthz
```

初始数据包含：

- 待发货订单 `TTS-20260801-1001`；
- 深圳仓缺货、洛杉矶仓有货的 SKU `SKU-BLACK-M-01`；
- 存在结算差异的账单 `STMT-2026-31`。

工具注册清单位于 `config/tools.json`。敏感工具在 JSON Schema 中声明 `x-kbot-approval`，由 kbot 生成领域化审批卡片。

## kbot 端到端课堂环境

```bash
make crossborder-up
make crossborder-install
make crossborder-e2e
```

`crossborder-install` 创建独立 Workspace、知识库、Tool、Skill、Agent 和离线 Eval。`crossborder-e2e` 验证订单诊断、敏感库存调拨、A2UI 审批、Checkpoint 恢复和审计链。

Compose 使用项目名 `kbot-crossborder`，数据库、Redis、Worker 和业务模拟器均与保险课堂环境隔离。若开发时复用外部 PostgreSQL/Redis，请为该环境分配独立 Redis DB，避免审批恢复任务被其他 Worker 消费。

默认入口为 `http://localhost:8181`，端口配置位于 `compose.env`。该环境可与保险项目同时运行。
