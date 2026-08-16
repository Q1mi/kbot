---
name: order_exception_recovery
description: 针对跨境订单缺货、超时和买家取消请求生成可审批的履约恢复方案
allowed-tools:
  - search_knowledge_base
  - get_order
  - get_inventory
  - get_shipping_options
  - create_inventory_transfer
  - approve_refund
allowed-kbs:
  - __KB_ID__
requires_network: true
---

# 前置校验

1. 使用 `search_knowledge_base` 检索订单异常等级、恢复顺序、调拨约束和升级路径。
2. 使用 `get_order` 确认订单状态为 `awaiting_shipment` 或 `partially_shipped`。
3. 使用 `get_inventory` 核验相关 SKU 的源仓可用量，预留库存不能进入调拨计算。
4. 使用 `get_shipping_options` 确认候选渠道满足 SLA。

# 决策顺序

1. 原履约仓可以按时发货时，保留原方案。
2. 替代物流可以满足 SLA 且成本合理时，优先给出物流切换建议。
3. 目标仓缺货且其他仓有真实可用量时，准备 `create_inventory_transfer` 参数。
4. 履约恢复不可行且订单满足退款状态约束时，准备 `approve_refund` 参数。

# 写操作约束

- 调拨数量不得超过源仓 `available`；
- 退款金额不得超过订单实付金额；
- 每项动作使用稳定 `idempotency_key`；
- 写工具调用进入人工审批，成功响应返回前保持“待审批”状态；
- 状态冲突、工具失败或证据缺失时停止写操作并转人工。
