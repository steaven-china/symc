// exp1-bus-nats: M1 真实 NATS 总线延迟(in-process baseline 对比,2026-06-20)
//
// 跟 cmd/exp1-bus 对比:in-process channel vs NATS pub/sub,差值 = 总线开销。
// NATS server 已在 D:/engine/symc/third-party/nats-data 跑起来(port 4222)。
package main

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/nats-io/nats.go"
)

var (
	payloadSizes  = []int{64, 1024, 64 * 1024}
	concurrencies = []int{1, 10, 100}
	iterations    = 1000
	natsURL       = "nats://127.0.0.1:4222"
)

type Result struct {
	PayloadSize int   `json:"payload_size"`
	Concurrency int   `json:"concurrency"`
	Iterations  int   `json:"iterations"`
	P50Ns       int64 `json:"p50_ns"`
	P99Ns       int64 `json:"p99_ns"`
	MeanNs      int64 `json:"mean_ns"`
	MinNs       int64 `json:"min_ns"`
	MaxNs       int64 `json:"max_ns"`
}

func main() {
	outDir := flag.String("out", "data/bus-nats", "")
	flag.Parse()
	start := time.Now()
	_ = os.MkdirAll(*outDir, 0o755)

	nc, err := nats.Connect(natsURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nats connect %s: %v\n", natsURL, err)
		os.Exit(1)
	}
	defer nc.Close()

	results := []Result{}
	for _, size := range payloadSizes {
		for _, conc := range concurrencies {
			r := bench(size, conc, nc)
			results = append(results, r)
			fmt.Printf("size=%-7d conc=%-4d p50=%-7dns p99=%-8dns mean=%-7dns min=%-7dns max=%-7dns\n",
				size, conc, r.P50Ns, r.P99Ns, r.MeanNs, r.MinNs, r.MaxNs)
		}
	}

	out := map[string]any{
		"results":       results,
		"elapsed_ms":    time.Since(start).Milliseconds(),
		"note":          "M1 真实 NATS pub/sub. Server localhost:4222.",
		"compare_with":  "in-process channel baseline (results/bus-baseline.json)",
	}
	f, _ := os.Create(filepath.Join(*outDir, "fit.json"))
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
	fmt.Printf("\nElapsed: %v\n", time.Since(start))
}

// bench 一个组合:N sender 各自 pub,1 个 sub 收
func bench(payloadSize, conc int, nc *nats.Conn) Result {
	subject := fmt.Sprintf("bench.%d.%d", payloadSize, conc)
	payload := make([]byte, payloadSize)
	for i := range payload {
		payload[i] = byte(i)
	}

	latenciesCh := make(chan int64, iterations)

	// 1 个订阅
	sub, err := nc.Subscribe(subject, func(msg *nats.Msg) {
		t0 := int64(binary.LittleEndian.Uint64(msg.Data[:8]))
		latenciesCh <- time.Now().UnixNano() - t0
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "subscribe: %v\n", err)
		return Result{PayloadSize: payloadSize, Concurrency: conc}
	}
	defer sub.Unsubscribe()

	time.Sleep(50 * time.Millisecond)

	// sender:每条消息前 8 字节写 t0
	for i := 0; i < iterations; i++ {
		t0 := time.Now().UnixNano()
		msg := make([]byte, 8+payloadSize)
		binary.LittleEndian.PutUint64(msg[:8], uint64(t0))
		copy(msg[8:], payload)
		if err := nc.Publish(subject, msg); err != nil {
			fmt.Fprintf(os.Stderr, "publish: %v\n", err)
		}
	}
	if err := nc.Flush(); err != nil {
		fmt.Fprintf(os.Stderr, "flush: %v\n", err)
	}

	latencies := make([]int64, 0, iterations)
	timeout := time.After(3 * time.Second)
	for i := 0; i < iterations; i++ {
		select {
		case l := <-latenciesCh:
			latencies = append(latencies, l)
		case <-timeout:
			i = iterations
		}
	}

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	r := Result{
		PayloadSize: payloadSize,
		Concurrency: conc,
		Iterations:  len(latencies),
	}
	if len(latencies) > 0 {
		r.P50Ns = latencies[len(latencies)*50/100]
		r.P99Ns = latencies[len(latencies)*99/100]
		r.MeanNs = mean(latencies)
		r.MinNs = latencies[0]
		r.MaxNs = latencies[len(latencies)-1]
	}
	return r
}

func mean(xs []int64) int64 {
	if len(xs) == 0 {
		return 0
	}
	s := int64(0)
	for _, x := range xs {
		s += x
	}
	return s / int64(len(xs))
}
