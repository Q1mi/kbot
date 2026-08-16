# kbot 课程交付仓库

本仓库交付《Go 企业级 AI Agent 平台实战》的逐课代码、最终稳定版源码和学员配套资料。

## 目录结构

```text
kbot-course/   23 课课堂代码，master 中保存 23-end 教学终态
kbot-final/    最终稳定版 kbot 完整源码
docs/          环境准备、逐课验收、版本对照与排障资料
```

## 跟随课程学习

每课提供 `XX-start` 和 `XX-end` 两个 Git 标签。切换标签后，代码位于 `kbot-course/`：

```bash
git switch --detach 01-start
git switch -c practice/01
cd kbot-course
go test ./...
```

完成练习后，可以与标准答案比较：

```bash
git diff 01-end -- kbot-course
```

课程顺序、每课核心增量和最短验收命令见 [课程地图](kbot-course/docs/course-map.md)。

## 使用最终稳定版

```bash
git switch master
cd kbot-final
make build
make test
```

完整课堂环境：

```bash
make bootstrap
make up
make demo
```

最终版的架构、配置与生产边界见 [kbot-final/README.md](kbot-final/README.md)。

## 配套资料

- [环境准备](docs/environment-setup.md)
- [每课验收命令](docs/lesson-verification.md)
- [课堂代码与最终代码能力对照](docs/course-to-final.md)
- [常见问题与排障](docs/troubleshooting.md)

## 分支与标签

| Git 入口 | 内容 |
|---|---|
| `master` | 完整交付目录与最终稳定版 |
| `course-history` | 逐课提交历史 |
| `01-start/end` 至 `23-start/end` | 每课起点与标准答案 |

仓库中的演示账号、固定课堂密钥和 Mock LLM Key 只用于本地 Compose 环境。接入真实模型或部署到共享环境前，请按照环境准备文档替换所有密钥。
