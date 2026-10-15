package store_test

import (
	"testing"

	"github.com/Roomen/cpa-live-routes/internal/wire"
)

func BenchmarkUsageHandle(b *testing.B) {
	s, _ := newStore(b, nil)
	usage := []byte(`{"RequestID":"u1","TraceID":"t1","Provider":"claude","BaseURL":"https://api.anthropic.com","ExecutorType":"claude","Model":"claude-opus-4-1","Alias":"opus","APIKey":
"sk-ant-abcdefghijklmnop3f9a","IsNativeKey":true,"AccessProvider":"config-inline","SessionID":"claude:7b1e2c4d-5a8f-4e0b","AuthID":"a.json","AuthIndex":"3","AuthType":"oauth","Source":"cli","Generate":true,"Stream":true,"RequestedAt":"2025-10-15T03:46:30.123456789Z","Latency":12300000000,"TTFT":1450000000,"Failed":false,"Failure":{"StatusCode":0,"Body":""},"Detail":{"InputTokens":48211,"OutputTokens":1320,"ReasoningTokens":640,"CachedTokens":40960,"TotalTokens":49531},"ResponseHeaders":{"Content-Type":["text/event-stream"]}}`)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		u, err := wire.ParseUsage(usage)
		if err != nil {
			b.Fatal(err)
		}
		s.Usage(u)
	}
}
