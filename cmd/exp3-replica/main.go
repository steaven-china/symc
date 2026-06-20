// exp3-replica: M3 读副本 broadcast 带宽(2026-06-20)
//
// 测"一个 primary region 写一次,广播给 N 个 cold 副本"的带宽增长。
// 目标:读副本数量上限 = N 超过多少改成"按需拉"而不是"广播推"。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

var (
	payloadSizes = []int{64, 1024, 64 * 1024}
	replicaCounts = []int{1, 4, 16, 64}
	duration     = 2 * time.Second // 每个组合跑多久
)

type Result struct {
	PayloadSize  int   `json:"payload_size"`
	Replicas     int   `json:"replicas"`
	TotalMsgs    int64 `json:"total_msgs"`
	TotalBytes   int64 `json:"total_bytes"`
	BandwidthMBps float64 `json:"bandwidth_mbps"`
	MsgRate      float64 `json:"msg_rate_per_sec"`
}

func main() {
	outDir := flag.String("out", "data/replica", "")
	flag.Parse()
	start := time.Now()
	_ = os.MkdirAll(*outDir, 0o755)

	results := []Result{}
	for _, size := range payloadSizes {
		for _, n := range replicaCounts {
			r := runBench(size, n)
			results = append(results, r)
			fmt.Printf("size=%-7d replicas=%-4d msgs=%-7d bw=%.2f MB/s msgs/s=%.0f\n",
				size, n, r.TotalMsgs, r.BandwidthMBps, r.MsgRate)
		}
	}

	out := map[string]any{
		"results":    results,
		"elapsed_ms": time.Since(start).Milliseconds(),
		"note":       "M3 broadcast bandwidth. real network: add 10-100x for cross-pod.",
	}
	f, _ := os.Create(filepath.Join(*outDir, "fit.json"))
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
	fmt.Printf("\nElapsed: %v\n", time.Since(start))
}

// runBench 一次组合
// primary 写 payload, N 个 replica 各自 channel 收
// 测 total bytes broadcast / duration
func runBench(payloadSize, replicas int) Result {
	payload := make([]byte, payloadSize)
	// 模拟 chunk 写 — 一次写 = 一次 broadcast
	// 用 channel 模拟"广播"——每个 replica 各自一个 channel
	chs := make([]chan []byte, replicas)
	for i := range chs {
		chs[i] = make(chan []byte, 1024) // buffered
	}

	var receivedBytes int64
	var receivedMsgs int64
	var wg sync.WaitGroup
	wg.Add(replicas)
	for i := 0; i < replicas; i++ {
		go func(idx int) {
			defer wg.Done()
			for msg := range chs[idx] {
				atomic.AddInt64(&receivedBytes, int64(len(msg)))
				atomic.AddInt64(&receivedMsgs, 1)
			}
		}(i)
	}

	// primary 写 — 跑 duration 这么久,尽可能快发
	t0 := time.Now()
	deadline := t0.Add(duration)
	for time.Now().Before(deadline) {
		// 一次写 = 一次 broadcast(往 N 个 channel 各发一份)
		for i := 0; i < replicas; i++ {
			chs[i] <- payload
		}
	}

	// 关闭所有 channel
	for i := range chs {
		close(chs[i])
	}
	wg.Wait()

	elapsed := time.Since(t0)
	totalBytes := atomic.LoadInt64(&receivedBytes)
	totalMsgs := atomic.LoadInt64(&receivedMsgs)

	// 带宽 = (replicas * msg_size * msg_count) / duration
	// 这里 totalBytes 已经包含 N 倍了(每个 replica 都收到一份)
	bytesPerSec := float64(totalBytes) / elapsed.Seconds()
	return Result{
		PayloadSize:   payloadSize,
		Replicas:      replicas,
		TotalMsgs:     totalMsgs,
		TotalBytes:    totalBytes,
		BandwidthMBps: bytesPerSec / 1024 / 1024,
		MsgRate:       float64(totalMsgs) / elapsed.Seconds(),
	}
}
