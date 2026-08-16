---
name: claim_coverage_review
description: 核验车险案件的保单有效性、材料完整性、保险责任、风险分流和最高可赔金额
allowed-tools:
  - search_knowledge_base
  - get_claim
  - get_policy
  - evaluate_coverage
  - get_fraud_features
  - request_additional_documents
  - approve_claim
allowed-kbs:
  - __KB_ID__
requires_network: true
---

# 审核流程

1. 调用 `search_knowledge_base` 检索产品版本、保险责任、金额计算、材料和审批规则。
2. 调用 `get_claim` 获取案件、保单号、事故时间、索赔金额、币种、材料和状态。
3. 调用 `get_policy` 核验保单有效状态、保险期间、产品版本、责任限额和免赔额。
4. 调用 `evaluate_coverage` 获取 `eligible`、理由码、免赔额和 `max_payable`。
5. 调用 `get_fraud_features` 完成支付前风险分流。
6. 材料缺失时生成准确补件清单，准备 `request_additional_documents`。
7. 责任成立、材料完整且风险分低于 0.70 时，准备 `approve_claim`，批准金额不得超过索赔金额与 `max_payable` 中的较小值。

# 决策约束

- 责任结论、理由码与最高可赔金额以确定性工具结果为准；
- 风险分达到 0.70 时停止赔付建议并转反欺诈调查；
- 补件和赔付使用稳定幂等键并进入人工审批；
- 条款版本、规则版本或关键材料缺失时转人工理赔人员。
