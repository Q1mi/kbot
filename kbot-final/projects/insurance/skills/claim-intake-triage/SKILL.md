---
name: claim_intake_triage
description: 核验保单、材料、责任和欺诈信号，将理赔案件路由到快速赔付、补件或人工调查
allowed-tools:
  - get_claim
  - get_policy
  - evaluate_coverage
  - get_fraud_features
  - request_additional_documents
  - hold_claim_payment
  - approve_claim
  - open_fraud_investigation
requires_network: true
---

## 执行流程

1. 调用 `get_claim` 和 `get_policy` 获取案件与保单事实。
2. 调用 `evaluate_coverage` 获取确定性责任判断、免赔额和最高可赔金额。
3. 调用 `get_fraud_features` 获取风险分、信号和规则版本。
4. 材料缺失时生成补件列表，并等待人工批准客户触达。
5. 风险分达到 0.70 时建议冻结支付，审批后再创建欺诈调查。
6. 责任成立、材料完整且风险分低于 0.70 时，批准金额不得超过 `max_payable`。
7. 所有写操作必须携带稳定的 `idempotency_key`。

## 决策约束

- Agent 不得修改规则引擎的责任结论和最高可赔金额。
- 任何拒赔、冻结和赔付操作都需要理由码、证据与人工审批。
- 条款版本、规则版本或关键材料缺失时转人工理赔审核。
