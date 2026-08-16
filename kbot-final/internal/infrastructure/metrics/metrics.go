// Package metrics 暴露 Prometheus 指标（设计文档 §4.11 / 讲义 §15.2）。
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// ChatRequests 对话请求总数。
	ChatRequests = promauto.NewCounter(prometheus.CounterOpts{
		Name: "kbot_chat_requests_total",
		Help: "Total number of chat requests handled by the runtime.",
	})
	// ToolCalls 工具调用总数（按工具名）。
	ToolCalls = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "kbot_tool_calls_total",
		Help: "Total number of tool calls, labeled by tool name.",
	}, []string{"tool"})
	// SkillTriggers 技能触发总数。
	SkillTriggers = promauto.NewCounter(prometheus.CounterOpts{
		Name: "kbot_skill_triggers_total",
		Help: "Total number of skill (L2) activations.",
	})
	// InjectionsBlocked 被 Guard 拦截的注入总数。
	InjectionsBlocked = promauto.NewCounter(prometheus.CounterOpts{
		Name: "kbot_injections_blocked_total",
		Help: "Total number of prompt-injection attempts blocked by Guard.",
	})
	// GuardBlocks Guard 拦截总数（按 hook）。
	GuardBlocks = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "kbot_guard_blocks_total",
		Help: "Total number of requests blocked by Guard, labeled by hook.",
	}, []string{"hook"})
	// AuditDropped 因队列满被丢弃的审计条数。
	AuditDropped = promauto.NewCounter(prometheus.CounterOpts{
		Name: "kbot_audit_dropped_total",
		Help: "Total number of audit events dropped due to a full queue.",
	})
)

// Handler 返回 Prometheus 指标 HTTP handler（挂在 /metrics）。
func Handler() http.Handler {
	return promhttp.Handler()
}
