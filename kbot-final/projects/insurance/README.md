# 保险承保、理赔与反欺诈 Agent

该目录是独立 Go Module，提供车险业务模拟系统、Tool 契约、Skill、条款知识库和 Eval 样本。它通过 HTTP Tool 接入 kbot，不导入 kbot 或跨境电商项目的 Go 包。

## 本地运行

```bash
make test
make run
curl http://localhost:8092/healthz
```

初始案件：

- `CLM-2026-0001`：材料完整、低风险的常规车损案件；
- `CLM-2026-0002`：事故时间早于保单生效时间，并命中重复影像信号的高风险案件。

Tool 清单位于 `config/tools.json`。赔付、支付冻结和欺诈立案均由 kbot 人工审批后执行。

## 独立课堂环境

在仓库根目录执行：

```bash
make insurance-up
make insurance-install
make insurance-e2e
```

`insurance-install` 创建本项目专属的 Workspace、知识库、Tool、Skill、Agent 和离线 Eval。`insurance-e2e` 验证高风险案件审核、赔款冻结、A2UI 审批、Checkpoint 恢复和审计链。

Compose 使用项目名 `kbot-insurance`，数据库、Redis、Worker 和业务模拟器均与跨境电商课堂环境隔离。若开发时复用外部 PostgreSQL/Redis，请为该环境分配独立 Redis DB，避免审批恢复任务被其他 Worker 消费。

默认入口为 `http://localhost:8182`，端口配置位于 `compose.env`。该环境可与跨境电商项目同时运行。
