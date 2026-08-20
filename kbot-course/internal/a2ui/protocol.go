// Package a2ui 定义 Agent 到前端的受限 UI 协议。
package a2ui

import "errors"

var ErrNotImplemented = errors.New("A2UI validation is implemented in 17-end")

type Node struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Props    map[string]any `json:"props,omitempty"`
	Children []Node         `json:"children,omitempty"`
}

type Document struct {
	Version string `json:"version"`
	Root    Node   `json:"root"`
}

func Validate(Document) error                              { return ErrNotImplemented }
func ApprovalCard(string, string, map[string]any) Document { return Document{} }
