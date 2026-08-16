---
name: settlement_reconciliation
description: 核验跨境平台结算差异并准备证据完整、可审计的财务申诉
allowed-tools:
  - search_knowledge_base
  - get_statement
  - create_reconciliation_case
allowed-kbs:
  - __KB_ID__
requires_network: true
---

# 执行步骤

1. 调用 `search_knowledge_base` 检索结算差异算式、申诉门槛、证据包和审批要求。
2. 调用 `get_statement` 获取账单状态、币种、`expected_amount` 与 `paid_amount`。
3. 使用确定性算式 `difference = expected_amount - paid_amount`，展示代入值和结果。
4. 仅在状态为 `difference_detected` 且 `difference > 0` 时准备申诉。
5. 申诉理由包含账单号、原币种、差异金额、费用项目、规则依据和证据来源。
6. 为 `create_reconciliation_case` 生成稳定幂等键并等待财务审批。

# 终止条件

- 账单不存在、币种不一致、差异小于等于零或状态不允许；
- 应结金额、实结金额或规则版本缺失；
- 工具结果冲突或申诉已经存在。
