package guard

import "github.com/Q1mi/kbot/internal/domain"

// Router 实现数据分级路由（设计文档 §4.6 / 讲义 §15.4）：根据本次请求内容的最高
// 数据分级与目标模型能处理的最高级别（classification_max）比较，过则放行，
// 不过则强制改路由到本地模型（secret 数据不出公司）。
type Router struct {
	// models: 模型别名 -> 该模型能处理的最高分级。
	models map[string]domain.Classification
	// localAlias: 兜底的本地模型别名（confidential/secret 强制走它）。
	localAlias string
}

// NewRouter 创建分级路由器。
func NewRouter(localAlias string) *Router {
	return &Router{models: make(map[string]domain.Classification), localAlias: localAlias}
}

// RegisterModel 登记某模型别名的 classification_max。
func (r *Router) RegisterModel(alias string, max domain.Classification) *Router {
	r.models[alias] = max
	return r
}

// RouteResult 路由决策结果。
type RouteResult struct {
	EffectiveAlias string
	Rerouted       bool
	Reason         string
}

// Route 给定请求的模型别名与内容最高分级，返回实际应使用的模型别名。
func (r *Router) Route(requestedAlias string, contentLevel domain.Classification) RouteResult {
	max, ok := r.models[requestedAlias]
	if !ok {
		// 未登记的模型保守按 internal 处理。
		max = domain.ClassInternal
	}
	if contentLevel.GreaterThan(max) {
		return RouteResult{
			EffectiveAlias: r.localAlias,
			Rerouted:       true,
			Reason:         "内容分级 " + string(contentLevel) + " 超过模型上限 " + string(max) + "，强制本地模型",
		}
	}
	return RouteResult{EffectiveAlias: requestedAlias, Rerouted: false}
}

// MaxClassification 返回一组分级里的最高级（默认 internal）。
func MaxClassification(levels ...domain.Classification) domain.Classification {
	highest := domain.ClassInternal
	for _, l := range levels {
		if l.GreaterThan(highest) {
			highest = l
		}
	}
	return highest
}
