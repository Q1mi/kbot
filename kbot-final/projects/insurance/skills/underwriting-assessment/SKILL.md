---
name: underwriting_assessment
description: 根据产品版本、车辆和历史风险信息生成可审计的人工核保建议
allowed-tools:
  - get_underwriting_case
  - assess_underwriting
  - approve_underwriting
requires_network: true
---

1. 调用 `get_underwriting_case` 获取车辆、历史出险、基础保费和当前状态。
2. 调用 `assess_underwriting` 获取规则集生成的风险等级、原因码和建议保费。
3. `refer_manual_underwriting` 案件必须进入人工核保。
4. 批准保费不得低于基础保费，写操作必须携带幂等键。
5. `approve_underwriting` 经 A2UI 审批后执行。
