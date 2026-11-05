package anonymizer

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func performanceText(kind string, size int) string {
	seed := "please review this ordinary document and explain the next steps\n"
	switch kind {
	case "code":
		seed = "func render(value string) { return value + \" ready\" } // review changes\n"
	case "dense":
		seed = "contact alice@example.test from 192.0.2.88 using https://private.test/path\n"
	case "unicode":
		seed = "请检查这段代码，说明处理流程与下一步操作。日志尚未提供。\n"
	}
	return strings.Repeat(seed, size/len(seed)+1)[:size]
}

func BenchmarkDetectPerformance(b *testing.B) {
	benchmarkDetection(b, Detect)
}

func BenchmarkReferenceDetectPerformance(b *testing.B) {
	benchmarkDetection(b, func(text string) []Match { return detectStructuredReference(text, nil) })
}

func benchmarkDetection(b *testing.B, detect func(string) []Match) {
	for _, kind := range []string{"plain", "code", "dense", "unicode"} {
		for _, size := range []int{10 << 10, 150 << 10} {
			text := performanceText(kind, size)
			b.Run(fmt.Sprintf("%s/%d", kind, size), func(b *testing.B) {
				b.SetBytes(int64(len(text)))
				b.ReportAllocs()
				for b.Loop() {
					detect(text)
				}
			})
		}
	}
}

func performanceRequest(slots, size int) []byte {
	messages := make([]any, slots)
	for i := range messages {
		messages[i] = map[string]any{"role": "user", "content": fmt.Sprintf("slot %d: ", i) + performanceText("code", size)}
	}
	data, _ := json.Marshal(map[string]any{"messages": messages})
	return data
}

func BenchmarkRequestPerformance(b *testing.B) {
	for _, shape := range []struct{ slots, size int }{{1, 150 << 10}, {200, 40}} {
		data := performanceRequest(shape.slots, shape.size)
		b.Run(fmt.Sprintf("slots_%d", shape.slots), func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for b.Loop() {
				if _, err := AnonymizeClaudeRequest(data, NewSession("perf", nil)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkColdRequestPerformance(b *testing.B) {
	previous := localDetectionCache
	localDetectionCache = newDetectionCache(0, 0, time.Minute)
	defer func() { localDetectionCache = previous }()
	BenchmarkRequestPerformance(b)
}

func BenchmarkParallelRequestThroughput(b *testing.B) {
	data := performanceRequest(1, 150<<10)
	if _, err := AnonymizeClaudeRequest(data, NewSession("perf-parallel", nil)); err != nil {
		b.Fatal(err)
	}
	samples := make([]time.Duration, 4096)
	var sampled atomic.Uint64
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			start := time.Now()
			if _, err := AnonymizeClaudeRequest(data, NewSession("perf-parallel", nil)); err != nil {
				b.Error(err)
				return
			}
			elapsed := time.Since(start)
			if i := sampled.Add(1) - 1; i < uint64(len(samples)) {
				samples[i] = elapsed
			}
		}
	})
	b.StopTimer()
	samples = samples[:min(uint64(len(samples)), sampled.Load())]
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	if len(samples) > 0 {
		b.ReportMetric(float64(samples[(len(samples)-1)*50/100])/float64(time.Millisecond), "p50_ms")
		b.ReportMetric(float64(samples[(len(samples)-1)*95/100])/float64(time.Millisecond), "p95_ms")
	}
}

func performanceStreamSession(count int) *Session {
	v := NewVault()
	for i := 0; i < count; i++ {
		v.byEngage["perf"] = append(v.byEngage["perf"], Mapping{Original: fmt.Sprintf("private-%d", i), Surrogate: fmt.Sprintf("alias-%08d", i)})
	}
	return NewSession("perf", v)
}

func BenchmarkStreamPerformance(b *testing.B) {
	for _, count := range []int{1, 1000} {
		for _, chunk := range []string{"ordinary output with no matching alias. ", "result alias-00000000 and more output. "} {
			b.Run(fmt.Sprintf("mappings_%d/hit_%t", count, strings.Contains(chunk, "alias-")), func(b *testing.B) {
				r := performanceStreamSession(count).NewStreamRestorer()
				b.SetBytes(int64(len(chunk)))
				b.ReportAllocs()
				for b.Loop() {
					r.Push("text", chunk)
				}
			})
		}
	}
}

func BenchmarkStreamSetupPerformance(b *testing.B) {
	for _, count := range []int{1, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			s := performanceStreamSession(count)
			b.ReportAllocs()
			for b.Loop() {
				s.NewStreamRestorer()
			}
		})
	}
}
