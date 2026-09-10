# 第 21、22、23 课代码快照

第 21、22 课使用新的教学主题与独立入口：

| 标签 | 课堂状态 | 验证重点 |
| --- | --- | --- |
| `21-start` | 第 20 课基础代码，加外部物流 Mock 和实验说明 | 启动 Mock、观察 AgentCard，为 A2A 客户端开发做准备 |
| `21-end` | A2A 客户端、统一工具分支、Runtime 物流演示完成 | `make lesson21-test`、`make a2a-demo` |
| `22-start` | 保留第 21 课成果，增加飞书凭据模板、消息接口和命令脚手架 | 创建应用，准备消息接入适配器 |
| `22-end` | 飞书长连接、身份映射、消息去重、Runtime 与回复完成 | `make lesson22-test`、`make feishu-demo` |
| `23-start` / `23-end` | 原最终交付课快照 | 按该快照原有 README、Makefile 和部署配置验收 |

`start` 的功能实现需要在课堂中完成；实验课件中的完整运行结果对应 `end`。

第 23 课的提交与文件树保持原值，其原有场景服务、Admin、Compose、CI 和 Sandbox 内容继续保留。新第 21、22 课移除旧的 Team、MCP、Webhook 签名实验及保险课程内容。第 23 课沿用原有独立交付快照，切换到该课时会恢复该快照包含的完整代码。

保存当前工作后，可以按标签进入对应课时：

```bash
git switch --detach 21-start
# 完成课堂实现后对照
git diff 21-start 21-end -- kbot-course
```

上面的 `git diff` 路径从 Git 仓库根目录执行；在 `kbot-course` 目录中使用 `git diff 21-start 21-end -- .`。
