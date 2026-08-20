// Package platform 是 Agent 控制面。
//
// 控制面负责配置、版本和发布。后续课程会逐步在这里加入 IAM、
// Tool、Prompt、Skill、知识库、Agent 和评测等服务。
package platform

// Platform 是控制面服务的装配入口。
// 当前课程先建立分层，具体服务会在后续版本中逐步加入。
type Platform struct{}

// New 创建控制面。
func New() *Platform {
	return &Platform{}
}
