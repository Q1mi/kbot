---
name: order_exception_triage
description: 识别跨境订单的库存、履约时效、物流和取消风险，并生成可执行恢复方案
allowed-tools:
  - get_order
  - get_inventory
  - get_shipping_options
  - create_inventory_transfer
  - approve_refund
requires_network: true
---

## 执行流程

1. 调用 `get_order` 获取订单状态、履约仓、商品和最晚发货时间。
2. 仅对 `awaiting_shipment` 或 `partially_shipped` 订单生成履约恢复方案。
3. 为每个 SKU 调用 `get_inventory`，核实库存可用量。
4. 调用 `get_shipping_options`，过滤无法满足 SLA 的渠道。
5. 按继续履约、跨仓调拨、取消退款的优先级生成方案，列出成本与风险。
6. 写操作必须携带稳定的 `idempotency_key`，并等待人工审批。

## 禁止事项

- 不得编造库存、物流价格和平台状态。
- 不得绕过订单状态机。
- 缺少订单、库存或 SLA 证据时转人工运营确认。
