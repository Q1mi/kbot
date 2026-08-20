# 保险理赔 Agent 实战

本项目提供一套可直接接入 Kbot 的垂直案例：

- `cmd/simulator` 暴露保单查询、欺诈评分、理赔决定三个 REST Tool；
- `config/tools.json` 固定 Tool Version ID、JSON Schema 与敏感操作审批元数据；
- `config/claim-skill.md` 固定理赔处理流程和 Tool allowlist；
- `config/agent.json` 给出 Agent Version 快照输入；
- `config/eval-cases.json` 用于第 20 课的版本发布门禁；
- `internal/claim` 保留金额、保单有效性、赔付限额等确定性领域规则。

运行 `make run` 启动 `:8092` 模拟器。Kbot Server 的 Tool allowlist 已包含
`insurance-sim`，Compose 环境可以直接使用配置中的内部地址。最终决定 Tool 标记为
敏感操作，会进入审批 checkpoint、A2UI、后台 Worker、审计链和恢复执行流程。

金额、限额和风险分会拒绝 NaN、Inf 与越界值；决定写入要求幂等键和证据，重复请求
返回同一结果，变更请求会得到冲突错误。
