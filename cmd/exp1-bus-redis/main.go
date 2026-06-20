// exp1-bus-redis: M1 Redis Streams 延迟(2026-06-20, miniredis mock)
//
// 跟 exp1-bus-nats 同样的 benchmark,改用 Redis Streams(走 miniredis 进程内 mock)。
// miniredis 跟 NATS 的对比 = "Go client + TCP + 协议序列化" vs "Go client + broker"。
// 注:miniredis 进程内 mock,无网络。真实 Redis 应再起独立进程。
package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

var (
	payloadSizes  = []int{64, 1024, 64 * 1024}
	concurrencies = []int{1, 10, 100}
	iterations    = 1000
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
	outDir := flag.String("out", "data/bus-redis", "")
	flag.Parse()
	start := time.Now()
	_ = os.MkdirAll(*outDir, 0o755)

	// 启 miniredis,端口 0 = OS 自动选空闲端口(原 6379 被占)
	mr := miniredis.NewMiniRedis()
	if err := mr.StartAddr("127.0.0.1:0"); err != nil {
		fmt.Fprintf(os.Stderr, "miniredis start: %v\n", err)
		os.Exit(1)
	}
	defer mr.Close()

	addr := mr.Addr()
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer rdb.Close()

	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		fmt.Fprintf(os.Stderr, "ping: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "miniredis listening on %s\n", addr)

	results := []Result{}
	for _, size := range payloadSizes {
		for _, conc := range concurrencies {
			r := bench(ctx, size, conc, rdb)
			results = append(results, r)
			fmt.Printf("size=%-7d conc=%-4d p50=%-7dns p99=%-8dns mean=%-7dns min=%-7dns max=%-7dns\n",
				size, conc, r.P50Ns, r.P99Ns, r.MeanNs, r.MinNs, r.MaxNs)
		}
	}

	out := map[string]any{
		"results":    results,
		"elapsed_ms": time.Since(start).Milliseconds(),
		"note":       "M1 Redis Streams via miniredis mock (in-process). For real Redis, run cmd/exp1-bus-redis-real.",
		"compare_with": "exp1-bus (in-process channel), exp1-bus-nats (real NATS)",
	}
	f, _ := os.Create(filepath.Join(*outDir, "fit.json"))
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
	fmt.Printf("\nElapsed: %v\n", time.Since(start))
}

// bench 一次组合:N sender 各自 XADD,1 个 consumer 收(读 stream 最新消息)
func bench(ctx context.Context, payloadSize, conc int, rdb *redis.Client) Result {
	streamKey := fmt.Sprintf("bench:%d:%d:%d", payloadSize, conc, time.Now().UnixNano())
	payload := make([]byte, payloadSize)
	for i := range payload {
		payload[i] = byte(i)
	}

	groupName := "bench-group"
	consumerName := fmt.Sprintf("c-%d", time.Now().UnixNano())

	// 创建 consumer group
	_ = rdb.XGroupCreateMkStream(ctx, streamKey, groupName, "$").Err()

	// 1 个 consumer goroutine
	latenciesCh := make(chan int64, iterations)
	go func() {
		for i := 0; i < iterations; i++ {
			res, err := rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
				Group:    groupName,
				Consumer: consumerName,
				Streams:  []string{streamKey, ">"},
				Count:    1,
				Block:    100 * time.Millisecond,
			}).Result()
			if err != nil {
				if err.Error() != "redis: nil" {
					return
				}
				continue
			}
			if len(res) == 0 || len(res[0].Messages) == 0 {
				continue
			}
			msg := res[0].Messages[0]
			t0Val, ok := msg.Values["t0"]
			if !ok {
				continue
			}
			t0Str, _ := t0Val.(string)
			if len(t0Str) < 8 {
				continue
			}
			var t0 int64
			for j := 0; j < 8; j++ {
				t0 |= int64(t0Str[j]) << (8 * j)
			}
			latenciesCh <- time.Now().UnixNano() - t0
		}
	}()

	time.Sleep(50 * time.Millisecond) // 等 consumer 起来

	// N sender,每个发 iterations/conc 次
	perSender := iterations / conc
	for i := 0; i < conc; i++ {
		go func() {
			for j := 0; j < perSender; j++ {
				t0 := time.Now().UnixNano()
				t0Bytes := make([]byte, 8)
				binary.LittleEndian.PutUint64(t0Bytes, uint64(t0))
				_ = rdb.XAdd(ctx, &redis.XAddArgs{
					Stream: streamKey,
					Values: map[string]any{
						"t0":  string(t0Bytes),
						"pld": string(payload),
					},
				}).Err()
			}
		}()
	}

	latencies := make([]int64, 0, iterations)
	timeout := time.After(5 * time.Second)
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
