# 课程知识库、Tool、Skill 与 Agent 预设

完整课堂环境为跨境电商和保险两个 Workspace 创建可直接检索的业务知识库、可试调的 Tool、流程型 Skill 与场景 Agent。两组资源使用独立的 Workspace、业务模拟器、Prompt、Model Profile 和知识空间。

## 跨境电商运营平台

### Tools

| Tool | 类型 | 业务用途 | 人工审批 |
|---|---|---|---|
| `get_order` | 查询 | 订单状态、市场、币种、商品、履约仓和最晚发货时间 | 无 |
| `get_inventory` | 查询 | SKU 各仓可用与预留库存 | 无 |
| `get_shipping_options` | 查询 | 承运商、费用、时效和 SLA 可行性 | 无 |
| `get_statement` | 查询 | 结算账单、应结与实结金额 | 无 |
| `create_inventory_transfer` | 写入 | 创建跨仓库存调拨 | 库存调拨审批 |
| `approve_refund` | 写入 | 批准符合状态与金额限制的订单退款 | 资金操作审批 |
| `create_reconciliation_case` | 写入 | 为正向结算差异创建平台申诉 | 财务申诉审批 |
| `search_knowledge_base` | 内部 SDK | 对 Agent 获准访问的业务规则执行混合检索 | 无 |

### 知识库

| 知识库 | 文档 | 核心内容 | 使用 Agent |
|---|---|---|---|
| 跨境电商商品运营知识库 | 履约诊断指南；取消、退款与数据边界 | 订单状态、库存口径、物流 SLA、退款资格、幂等和转人工 | 商品运营 Agent |
| 跨境电商供应链协同知识库 | 库存物流恢复 SOP；结算对账规范 | 异常分级、跨仓调拨、物流选择、差异计算、证据包和申诉 | 供应链协同 Agent |

### Skills

| Skill | 核心流程 | 使用 Agent |
|---|---|---|
| `product_fulfillment_diagnosis` | 订单→库存→SLA→履约方案排序 | 商品运营 Agent |
| `order_exception_recovery` | 异常订单→库存与物流核验→调拨/退款待审批 | 供应链协同 Agent |
| `settlement_reconciliation` | 账单→确定性差异计算→财务申诉待审批 | 供应链协同 Agent |

### Agents

| Agent | System Prompt | User Prompt Template | Tools | Skills | KB | Max steps |
|---|---|---|---:|---:|---:|---:|
| 商品运营 Agent | `商品运营 · System Prompt@dev` | `商品运营 · User Prompt Template@dev` | 6 | 1 | 1 | 8 |
| 供应链协同 Agent | `供应链协同 · System Prompt@dev` | `供应链协同 · User Prompt Template@dev` | 8 | 2 | 1 | 12 |

## 保险理赔与反欺诈平台

### Tools

| Tool | 类型 | 业务用途 | 人工审批 |
|---|---|---|---|
| `get_policy` | 查询 | 保单状态、产品版本、保险期间、限额与免赔额 | 无 |
| `get_claim` | 查询 | 事故、报案、索赔金额、材料和案件状态 | 无 |
| `evaluate_coverage` | 确定性规则 | 责任、理由码、免赔额和最高可赔金额 | 无 |
| `get_fraud_features` | 规则查询 | 风险分、命中信号和规则集版本 | 无 |
| `request_additional_documents` | 写入 | 发起理赔补件请求 | 客户触达审批 |
| `approve_claim` | 写入 | 批准符合责任与金额约束的赔付 | 资金操作审批 |
| `hold_claim_payment` | 写入 | 冻结达到风险阈值的案件支付 | 高风险案件审批 |
| `open_fraud_investigation` | 写入 | 为已冻结案件创建欺诈调查任务 | 调查立案审批 |
| `search_knowledge_base` | 内部 SDK | 对 Agent 获准访问的业务规则执行混合检索 | 无 |

### 知识库

| 知识库 | 文档 | 核心内容 | 使用 Agent |
|---|---|---|---|
| 保险理赔审核知识库 | 碰撞险责任与赔款计算；材料审核与审批 SOP | 产品版本、保险期间、理由码、金额公式、补件和赔付门槛 | 理赔审核 Agent |
| 保险反欺诈分析知识库 | 风险信号与证据规范；调查、公平性与处置 SOP | 0.70 阈值、信号释义、冻结立案、证据边界、公平性和审计 | 反欺诈分析 Agent |

### Skills

| Skill | 核心流程 | 使用 Agent |
|---|---|---|
| `claim_coverage_review` | 案件→保单→责任→风险分流→补件/赔付 | 理赔审核 Agent |
| `fraud_risk_triage` | 事实时间线→责任边界→信号证据→支付分流 | 反欺诈分析 Agent |
| `fraud_investigation_coordination` | 风险阈值→冻结审批→调查立案审批 | 反欺诈分析 Agent |

### Agents

| Agent | System Prompt | User Prompt Template | Tools | Skills | KB | Max steps |
|---|---|---|---:|---:|---:|---:|
| 理赔审核 Agent | `理赔审核 · System Prompt@dev` | `理赔审核 · User Prompt Template@dev` | 7 | 1 | 1 | 10 |
| 反欺诈分析 Agent | `反欺诈分析 · System Prompt@dev` | `反欺诈分析 · User Prompt Template@dev` | 7 | 2 | 1 | 10 |

## 检索与权限设计

8 份 Markdown 文档通过统一 Connector ingest 管道切分并写入 pgvector。`search_knowledge_base` 作为 `internal_sdk` Tool 注册到每个 Workspace，运行时执行关键词、向量和 RRF 混合检索。

每个 Agent 快照只挂载一个场景知识库，Skill 的 `allowed-kbs` 使用同一 KB ID。Agent 与 Skill 两层 allowlist 会限制可检索范围，跨 Workspace 的知识库 ID 无法通过发布和运行时校验。

## 初始化与发布门禁

`make up` 在完整课堂环境中执行以下流程：

1. 等待 `crossborder-sim` 和 `insurance-sim` 通过健康检查。
2. 幂等创建 4 个知识库，增量同步 8 份内嵌文档并交给 Worker ingest。
3. 幂等创建缺失的 Tool v1；15 个 REST Tool 完成真实试调，写 Tool 使用 `dry_run=true`。
4. 两个 `search_knowledge_base` Tool 通过内部 SDK 检索试调，共发布 17 个 Tool。
5. Tool 发布后执行 Skill 的 `allowed-tools` 与 `allowed-kbs` 门禁校验。
6. 将已发布 Skill Version、Tool Version、KB ID、System Prompt 与 User Prompt Template 环境指针固化到 Agent 版本。
7. 系统维护的旧版 Skill 与 Agent 会生成新的不可变版本；学员创建的后续版本保持原状。
8. 文档内容 Hash 未变化且处理完成时，服务重启会跳过重复 ingest。

轻量环境默认设置 `KBOT_AUTOSEED_COURSE_ASSETS=false`，便于只启动基础控制面。需要自定义业务模拟器时，可启用该开关并保证容器网络内的默认服务地址可访问。

## 源文件

- [幂等初始化与 Agent 装配](../internal/platform/coursepreset/business_assets.go)
- [跨境电商 Tool 契约](../internal/platform/coursepreset/assets/crossborder.tools.json)
- [保险 Tool 契约](../internal/platform/coursepreset/assets/insurance.tools.json)
- [Skill 资产目录](../internal/platform/coursepreset/assets/skills)
- [课程知识库资产目录](../internal/platform/coursepreset/assets/knowledge)
