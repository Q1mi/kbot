# kbot 课程代码检查点

当前提交对应 kbot 课程的一个 `XX-start` 或 `XX-end` 检查点，课堂代码统一位于 `kbot-course/`。

```bash
cd kbot-course
go test ./...
```

- `XX-start`：本课开始前的最小可运行代码。
- `XX-end`：完成本课核心功能后的参考代码。
- [课程地图](kbot-course/docs/course-map.md)

建议从开始标签创建个人练习分支：

```bash
git switch --detach 01-start
git switch -c practice/01
cd kbot-course
```
