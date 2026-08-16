---
name: settlement_reconciliation
description: 核验跨境平台结算差异并创建带证据的申诉单
allowed-tools:
  - get_statement
  - create_reconciliation_case
requires_network: true
---

## 执行流程

1. 调用 `get_statement` 获取应结金额、实结金额、币种和状态。
2. 使用确定性算式 `expected_amount - paid_amount` 计算差异。
3. 仅在状态为 `difference_detected` 且差异大于零时生成申诉建议。
4. 在理由中写明费用项目、差异金额和证据来源。
5. `create_reconciliation_case` 必须等待财务人员审批。
