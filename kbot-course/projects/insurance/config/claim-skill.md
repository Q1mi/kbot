---
name: insurance-claim
description: 处理保险理赔材料核验、风险分级和赔付决定
allowed-tools: [get_policy, score_claim_fraud, submit_claim_decision]
requires_network: true
max-steps: 8
---
先调用 get_policy 校验保单状态与赔付限额，再检查材料是否完整。
对满足基本条件的理赔调用 score_claim_fraud。金额和限额使用工具返回的确定性数据，
不得自行猜测。输出决定时列出 policy、金额检查、材料检查和 risk_score 证据。
只有用户明确要求提交决定时才调用 submit_claim_decision；该 Tool 会进入人工审批。
