// Package util 提供通用工具函数
package util

import "github.com/google/uuid"

// GenerateID 生成随机ID
func GenerateID() string {
	return uuid.NewString()
}
