---
name: product_fulfillment_diagnosis
description: 基于订单、SKU 库存与物流 SLA 诊断商品关联订单的履约风险并生成运营优先级
allowed-tools:
  - search_knowledge_base
  - get_order
  - get_inventory
  - get_shipping_options
allowed-kbs:
  - __KB_ID__
requires_network: true
---

# 适用范围

用于商品运营人员处理指定订单和 SKU 的缺货、发货超时、物流不可达与成本异常。涉及流量、广告、标题、转化率或竞品判断时，先声明当前工具的数据缺口。

# 执行步骤

1. 调用 `search_knowledge_base` 检索目标市场的履约规则、状态约束、SLA 和转人工条件。
2. 调用 `get_order`，核验订单状态、市场、币种、商品明细、履约仓和 `ship_by`。
3. 确认目标 SKU 属于该订单，再调用 `get_inventory` 查询各仓 `available` 与 `reserved`。
4. 调用 `get_shipping_options`，过滤 `sla_eligible=false` 的渠道。
5. 按原仓履约、替代物流、跨仓调拨、取消退款的顺序比较方案。
6. 输出每个候选的适用条件、知识依据、库存证据、时效、成本与失败风险。

# 完成条件

- 所有事实均带工具来源；
- 库存判断只使用 `available`；
- 可执行物流只包含满足 SLA 的候选；
- 数据不足时给出最小补充信息清单并转人工运营确认。
