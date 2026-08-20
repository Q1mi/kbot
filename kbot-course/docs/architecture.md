# kbot 最小架构

第 01 课先建立四层依赖方向：

```text
API
 ↓
Platform（控制面：配置、版本、发布）
 ↓ immutable snapshot
Runtime（数据面：对话、检索、工具执行）
 ↓
Infrastructure
```

当前版本只定义控制面与 Runtime 之间的稳定接口。

Conversation 固定 AgentVersion ID。Runtime 每次都按照这个固定版本解析
AgentSnapshot，因此控制面发布新版本后，已有会话仍能保持原有行为。
