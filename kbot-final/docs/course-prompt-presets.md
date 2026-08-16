# 课程业务 Prompt 预设

课堂环境为跨境电商和保险两个 Workspace 预置四套业务 Prompt。每套包含一个 System Prompt 和一个 User Prompt Template，可直接用于 Prompt 工程、模型配置、Agent 装配、人在环审批与评测课程。

## 预设清单

| Workspace | 业务场景 | System Prompt | User Prompt Template | 绑定 Profile |
|---|---|---|---|---|
| 跨境电商运营平台 | 商品运营 | `商品运营 · System Prompt` | `商品运营 · User Prompt Template` | `商品运营 Profile v1` |
| 跨境电商运营平台 | 供应链协同 | `供应链协同 · System Prompt` | `供应链协同 · User Prompt Template` | `供应链协同 Profile v1` |
| 保险理赔与反欺诈平台 | 理赔审核 | `理赔审核 · System Prompt` | `理赔审核 · User Prompt Template` | `理赔审核 Profile v1` |
| 保险理赔与反欺诈平台 | 反欺诈分析 | `反欺诈分析 · System Prompt` | `反欺诈分析 · User Prompt Template` | `反欺诈分析 Profile v1` |

System Prompt 版本包含场景化生成参数，并在创建时指向对应的 Model Profile Version。User Prompt Template 绑定到 Agent 版本，用于把 Playground 首轮业务表单渲染成标准 `user` 消息。模板仍可在 Prompt Center 中独立调试，便于学员观察 `text/template` 变量、JSON Schema 必填校验和 `missingkey=error` 行为。

## 设计边界

四个 System Prompt 统一覆盖以下工程要求：

- 业务角色、任务目标和权威数据源；
- 顺序明确的工具调用流程与业务状态校验；
- 确定性金额、库存、SLA 和风险阈值规则；
- 写操作的明确执行意图、稳定幂等键和人工审批；
- Prompt Injection 隔离、数据最小化和工具失败转人工；
- 可审计输出结构，仅展示结论、证据、必要计算和操作记录。

反欺诈场景使用 `0.70` 课程风险阈值。风险信号用于分流与调查，最终处置需要确定性责任规则、调查证据和授权人员共同支持。

## User Prompt Template 变量

| 场景 | 必填变量 |
|---|---|
| 商品运营 | `market`, `order_id`, `sku`, `objective`, `execution_mode`, `constraints` |
| 供应链协同 | `task_type`, `primary_resource_id`, `sku`, `objective`, `execution_mode`, `constraints` |
| 理赔审核 | `claim_id`, `review_goal`, `execution_mode`, `operator_instruction`, `additional_context` |
| 反欺诈分析 | `claim_id`, `analysis_goal`, `execution_mode`, `investigation_scope`, `additional_context` |

课堂预设同时在 Variables Schema 中保存一组可直接运行的演示值。Playground 选中 Agent 后会自动回填；学员仍可修改任意字段。

| 场景 | 默认业务对象 | 默认任务 | 默认执行模式 |
|---|---|---|---|
| 商品运营 | `US` / `TTS-20260801-1001` / `SKU-BLACK-M-01` | 诊断商品关联订单的履约风险并给出运营优先级 | `analyze_only` |
| 供应链协同 | `order_exception` / `TTS-20260801-1001` / `SKU-BLACK-M-01` | 生成可执行的跨仓调拨恢复方案 | `prepare_action` |
| 理赔审核 | `CLM-2026-0001` | 核验责任并给出最高可赔金额 | `analyze_only` |
| 反欺诈分析 | `CLM-2026-0002` | 评估欺诈风险并生成调查处置建议 | `prepare_action` |

服务启动时会为缺少这些默认值的课程 User Prompt 创建新版本并晋升 `dev`，保留已有模板正文、Schema 约束与学员版本。

`execution_mode` 支持：

- `analyze_only`：查询、分析并输出建议；
- `prepare_action`：生成待审批的工具参数与幂等键；
- `execute_after_approval`：核验平台审批状态后执行受控写操作。

## Agent 与会话中的生效方式

Agent Builder 将两类 Prompt 分开配置：

- System Prompt 来源支持“绑定 Prompt Center”和“使用字面量”，两种来源互斥；
- “绑定 System Prompt”只展示系统类 Prompt；
- User Prompt Template 使用独立绑定项，只展示 `*-user-template` 类别；
- Prompt 环境同时决定 System Prompt 与 User Prompt Template 在新会话中解析的环境指针。

创建新会话时，Playground 读取 Agent 当前环境的不可变版本，并根据 User Prompt Template 的 Variables Schema 生成首轮业务任务表单。提交后由服务端完成模板渲染，将结果保存为首条 `user` 消息；输入框中的文本会作为“补充说明”追加。后续轮次继续发送普通用户消息。

会话运行配置会固化以下审计信息：

- 实际命中的 User Prompt Version ID；
- 本轮提交的模板变量；
- 服务端渲染后的完整 User Prompt；
- 渐进发布实验与变体信息（存在时）。

客户端提交不可变 User Prompt Version ID，可避免用户填写表单期间环境指针变化导致模板和 Schema 错配。服务端还会校验模板归属、Agent 绑定关系和 Workspace 边界。

## 源文件

- [商品运营 System Prompt](../internal/platform/coursepreset/prompts/crossborder_product_operations.system.md) / [User Prompt Template](../internal/platform/coursepreset/prompts/crossborder_product_operations.user.md) / [Variables Schema](../internal/platform/coursepreset/prompts/crossborder_product_operations.schema.json)
- [供应链协同 System Prompt](../internal/platform/coursepreset/prompts/crossborder_supply_chain.system.md) / [User Prompt Template](../internal/platform/coursepreset/prompts/crossborder_supply_chain.user.md) / [Variables Schema](../internal/platform/coursepreset/prompts/crossborder_supply_chain.schema.json)
- [理赔审核 System Prompt](../internal/platform/coursepreset/prompts/insurance_claim_review.system.md) / [User Prompt Template](../internal/platform/coursepreset/prompts/insurance_claim_review.user.md) / [Variables Schema](../internal/platform/coursepreset/prompts/insurance_claim_review.schema.json)
- [反欺诈分析 System Prompt](../internal/platform/coursepreset/prompts/insurance_fraud_analysis.system.md) / [User Prompt Template](../internal/platform/coursepreset/prompts/insurance_fraud_analysis.user.md) / [Variables Schema](../internal/platform/coursepreset/prompts/insurance_fraud_analysis.schema.json)

## 初始化与教学使用

1. 服务启动时先初始化 Workspace 和模型 Profile。
2. `coursepreset.EnsurePrompts` 检查目标 Workspace 和 Profile v1，为缺失的预设创建 Prompt v1 并指向 `dev` 环境。
3. 重启时保留同名 Prompt 及学员创建的后续版本。
4. 在 Agent Builder 中选择相应 System Prompt 与 User Prompt Template，再装配该场景的 Tool、Skill 和知识库。
5. 进入会话 Playground，填写自动生成的首轮业务任务表单并发起任务。
6. 在会话详情或 Langfuse Trace 中核对模板版本、变量、渲染消息和后续工具调用。

Prompt 对应的 Tool、Skill 和 Agent 装配见 [课程 Tool、Skill 与 Agent 预设](course-business-assets.md)。
