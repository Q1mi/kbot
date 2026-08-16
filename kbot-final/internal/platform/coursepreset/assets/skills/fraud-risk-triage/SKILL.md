---
name: fraud_risk_triage
description: 汇总理赔事实、责任结论和规则信号，完成反欺诈风险评分解释与支付分流
allowed-tools:
  - search_knowledge_base
  - get_claim
  - get_policy
  - evaluate_coverage
  - get_fraud_features
  - hold_claim_payment
allowed-kbs:
  - __KB_ID__
requires_network: true
---

# 分析流程

1. 调用 `search_knowledge_base` 检索当前规则集的信号释义、证据要求、阈值与公平性边界。
2. 调用 `get_claim` 与 `get_policy` 建立事故时间、保险期间、报案时间、金额和材料时间线。
3. 调用 `evaluate_coverage`，单独记录保险责任结论、理由码和产品版本。
4. 调用 `get_fraud_features`，原样记录 `risk_score`、`signals` 与 `rule_set`。
5. 将每个信号映射到现有证据、待核验假设和建议调查动作。
6. 风险分低于 0.70 时返回常规审核；达到或超过 0.70 时停止自动赔付建议并准备支付冻结。

# 风险边界

- 风险信号支持案件分流和调查立项；
- 最终处置需要确定性责任规则、调查证据和授权人员共同支持；
- 不使用与保险风险无关的敏感个人属性；
- `hold_claim_payment` 需要稳定幂等键、明确理由和人工审批。
