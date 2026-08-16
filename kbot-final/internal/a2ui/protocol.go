// Package a2ui 提供 A2UI v0.9.1（线协议版本字段 v0.9）的受控消息模型。
//
// kbot 只允许服务端声明式组件通过已审核 catalog 渲染，消息中不携带可执行代码。
package a2ui

import "time"

const (
	// Version 遵循 A2UI v0.9.1 JSON Schema 中规定的 wire version。
	Version = "v0.9"
	// BasicCatalog 是官方 v0.9 基础组件目录。
	BasicCatalog = "https://a2ui.org/specification/v0_9/catalogs/basic/catalog.json"
	MIMEType     = "application/a2ui+json"
)

const (
	ActionApprove = "approval.approve"
	ActionReject  = "approval.reject"
)

// Message 是 Server-to-Client 四种 envelope 的联合类型。
type Message struct {
	Version          string            `json:"version"`
	CreateSurface    *CreateSurface    `json:"createSurface,omitempty"`
	UpdateComponents *UpdateComponents `json:"updateComponents,omitempty"`
	UpdateDataModel  *UpdateDataModel  `json:"updateDataModel,omitempty"`
	DeleteSurface    *DeleteSurface    `json:"deleteSurface,omitempty"`
}

type CreateSurface struct {
	SurfaceID     string         `json:"surfaceId"`
	CatalogID     string         `json:"catalogId"`
	Theme         map[string]any `json:"theme,omitempty"`
	SendDataModel bool           `json:"sendDataModel,omitempty"`
}

type UpdateComponents struct {
	SurfaceID  string      `json:"surfaceId"`
	Components []Component `json:"components"`
}

type UpdateDataModel struct {
	SurfaceID string `json:"surfaceId"`
	Path      string `json:"path,omitempty"`
	Value     any    `json:"value,omitempty"`
}

type DeleteSurface struct {
	SurfaceID string `json:"surfaceId"`
}

// Component 覆盖 kbot 企业 catalog 中启用的 A2UI 基础组件子集。
// Dynamic 属性用 any 表示字面量或 {"path":"/json-pointer"} 数据绑定。
type Component struct {
	ID        string   `json:"id"`
	Component string   `json:"component"`
	Text      any      `json:"text,omitempty"`
	Variant   string   `json:"variant,omitempty"`
	Children  []string `json:"children,omitempty"`
	Child     string   `json:"child,omitempty"`
	Justify   string   `json:"justify,omitempty"`
	Align     string   `json:"align,omitempty"`
	Axis      string   `json:"axis,omitempty"`
	Action    *Action  `json:"action,omitempty"`
}

type Action struct {
	Event *ActionEvent `json:"event,omitempty"`
}

type ActionEvent struct {
	Name    string         `json:"name"`
	Context map[string]any `json:"context,omitempty"`
}

// ClientMessage 是 v0.9.1 Client-to-Server action/error envelope。
type ClientMessage struct {
	Version string        `json:"version"`
	Action  *ClientAction `json:"action,omitempty"`
	Error   *ClientError  `json:"error,omitempty"`
}

type ClientAction struct {
	Name              string         `json:"name"`
	SurfaceID         string         `json:"surfaceId"`
	SourceComponentID string         `json:"sourceComponentId"`
	Timestamp         time.Time      `json:"timestamp"`
	Context           map[string]any `json:"context"`
}

type ClientError struct {
	Code      string `json:"code"`
	SurfaceID string `json:"surfaceId"`
	Path      string `json:"path,omitempty"`
	Message   string `json:"message"`
}
