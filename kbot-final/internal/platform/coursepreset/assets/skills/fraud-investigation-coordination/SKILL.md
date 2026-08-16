---
name: fraud_investigation_coordination
description: 为已达到高风险阈值的理赔案件准备支付冻结与欺诈调查立案方案
allowed-tools:
  - search_knowledge_base
  - get_claim
  - get_fraud_features
  - hold_claim_payment
  - open_fraud_investigation
allowed-kbs:
  - __KB_ID__
requires_network: true
---

# 前置条件

1. 调用 `search_knowledge_base` 检索冻结门槛、调查证据、公平性控制和最终处置边界。
2. 调用 `get_claim` 确认案件仍处于允许风险处置的状态。
3. 调用 `get_fraud_features` 确认风险分达到 0.70，并记录规则集版本和信号。
4. 为每个信号列出支持证据与待调查范围，避免把待核验假设写成已证实事实。

# 受控动作

1. 先准备 `hold_claim_payment`，理由包含风险分、阈值、规则集和主要信号。
2. 冻结工具成功返回后，等待新的明确执行意图，再准备 `open_fraud_investigation`。
3. 两项动作分别使用稳定幂等键并分别经过人工审批。
4. 记录冻结操作 ID、调查任务 ID、审批状态和下一责任人。

# 转人工条件

- 风险分或规则集缺失；
- 案件状态不允许冻结；
- 信号证据相互矛盾；
- 冻结操作失败或尚未完成。
