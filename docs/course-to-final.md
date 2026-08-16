# 课堂代码与最终代码能力对照

本仓库同时交付 `kbot-course/` 课堂实现和 `kbot-final/` 最终稳定版。课程代码强调核心机制的最小闭环，最终版补齐企业工程约束、异步基础设施、安全治理和运行证据。

## Git 入口

| 内容 | 入口 | 完成标准 |
|---|---|---|
| 单课 Starter | `XX-start` | 能编译，已有测试通过，本课核心行为待完成 |
| 单课答案 | `XX-end` | 本课测试和演示通过 |
| 教学终态 | `23-end` | 23 项核心能力与教学 Compose 可运行 |
| 最终稳定版 | `master` 下的 `kbot-final/` | 完整控制面、异步任务、治理、安全和课堂环境 |

查看某课差异：

```bash
git diff 09-start..09-end -- kbot-course
```

## 能力映射

| `kbot-course/` 教学终态 | `kbot-final/` 最终稳定版 |
|---|---|
| 单组核心 migration，手写 PostgreSQL Store | 26 组 migration、sqlc 查询和 Memory/PostgreSQL contract test |
| 进程内任务与审批恢复 | Redis、Asynq Worker、租约、fencing token、补偿扫描和有界重试 |
| 教学版 BM25、向量和 RRF | PostgreSQL FTS、pgvector、异步 ingest、三档检索 Playground |
| 基础 Workspace 成员和角色 | owner/admin/editor/member/viewer 五档 RBAC 与跨空间资源校验 |
| Tool 版本和基础执行门禁 | 凭据 AES-GCM 加密、JSON Schema 校验、逐跳 SSRF 校验与结构化执行审计 |
| 独立 Sandbox Runner | 镜像 digest、预拉取、孤儿清理、容量快速失败和完整 readiness |
| Prompt 与 Model Profile 快照 | Provider/Deployment/Profile 主备路由、项目绑定、RPM/TPM/月预算结算 |
| 审批 Checkpoint 与 A2UI | approval ID 唯一绑定、会话执行锁、失败恢复和单次副作用保证 |
| 基础 OTel 与审计链 | Langfuse Trace 深链、分区审计、Tool/Skill/Sandbox 版本归因 |
| 确定性 Eval 门禁 | 确定性、LLM light、LLM full Judge、历史运行和逐用例结果 |
| 基础 Compose | 完整课堂、轻量开发、跨境和保险四套隔离环境 |

## 推荐学习顺序

1. 按 `01-start/end` 至 `23-start/end` 完成课堂核心实现；
2. 在 `23-end` 的 `kbot-course/` 中运行 `make verify`；
3. 切换 `master`，进入 `kbot-final/` 对照同名包和接口；
4. 优先阅读 PostgreSQL/Redis/Asynq、审批恢复、Tool 安全和模型治理；
5. 用最终 Compose 完成跨境或保险的一条审批 E2E；
6. 选择一个模块进行二次开发，并补齐对应测试和文档。

生产部署仍需结合组织要求补充 SSO/OIDC、外部 Secret Manager、Kubernetes、HA、备份和灾备。
