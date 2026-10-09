# 课程课件索引

`master` 集中收录各个 `XX-end` 标签中的 Markdown 课件，方便连续阅读。每课的代码和实验命令以对应标签为准；运行实验前，请保存当前工作并切换到下表中的代码标签。

## 分课课件

| 课时 | 课件 | 文档来源 | 实验代码 |
| --- | --- | --- | --- |
| 19 | [OpenTelemetry Trace 与 Langfuse](langfuse.md) | `19-end` | [`19-end`](https://github.com/Q1mi/kbot/tree/19-end/kbot-course) |
| 21 | [A2A 协议与外部物流服务示例](lesson-21-a2a-logistics.md) | `22-end` | [`21-end`](https://github.com/Q1mi/kbot/tree/21-end/kbot-course) |
| 22 | [飞书创建应用与接入 kbot](lesson-22-feishu-kbot.md) | `22-end` | [`22-end`](https://github.com/Q1mi/kbot/tree/22-end/kbot-course) |

第 21 课采用 `22-end` 中补全第 22 课链接的修订版，代码和示例链接指向 `21-end`。各课文档中的代码链接均定位到对应标签，课件之间使用本目录的相对链接。

目前各 `XX-end` 标签中提供的独立课件如上；其余课时的主题、代码入口和验收命令见[课程地图](course-map.md)。

## 公共资料

- [课程地图](course-map.md)：23 节课的主题、代码入口和验收方式。
- [第 21、22、23 课代码快照](lesson-snapshots.md)：起止标签、新课范围和第 23 课快照说明，来源为 `22-end`。
- [平台架构](architecture.md)：`master` 保留的整体架构说明。
- [集成说明](integrations.md)：`master` 交付代码中的 Team、MCP、A2A、Webhook 与飞书集成。

## 运行课程实验

例如运行第 21 课，在 Git 仓库根目录执行：

```bash
# 先保存当前工作区修改
git fetch origin --tags
git switch --detach 21-end
cd kbot-course
make lesson21-test
```

第 19、22 课分别切换到 `19-end`、`22-end`，再按课件准备配置并运行。`master` 的课程代码沿用最终交付版本，分课演示命令需在对应标签中执行。
