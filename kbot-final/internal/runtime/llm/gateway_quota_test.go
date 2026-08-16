package llm

import (
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestEstimateMessageTokensUsesConservativeMixedLanguageBaseline(t *testing.T) {
	messages := []*schema.Message{{Role: schema.User, Content: "请分析这个跨境订单并给出库存调拨建议"}}
	tokens := estimateMessageTokens(messages)
	if tokens < 20 {
		t.Fatalf("mixed-language token estimate is unexpectedly low: %d", tokens)
	}
}
