// exp1-bus: M1 事件总线延迟基线(in-process channel,2026-06-20)
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

func main() {
	outDir := flag.String("out", "data/bus", "")
	flag.Parse()
	start := time.Now()
	_ = os.MkdirAll(*outDir, 0o755)

	concs := []int{1, 10, 100}
	iters := 1000
	results := []map[string]any{}

	for _, c := range concs {
		// 简化: 1 个 sender goroutine, 1 个 receiver goroutine
		// 测 send→recv 单程延迟
		ch := make(chan time.Time, iters)
		latencies := make([]int64, 0, iters)

		// receiver goroutine
		done := make(chan struct{})
		go func() {
			for i := 0; i < iters; i++ {
				t0 := <-ch
				latencies = append(latencies, time.Since(t0).Nanoseconds())
			}
			close(done)
		}()

		// sender
		perSender := iters / c
		for s := 0; s < c; s++ {
			go func(n int) {
				for j := 0; j < n; j++ {
					ch <- time.Now()
				}
			}(perSender)
		}

		<-done
		// drain + close
		close(ch)

		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		r := map[string]any{
			"concurrency": c,
			"iterations":  len(latencies),
			"p50_ns":      latencies[len(latencies)*50/100],
			"p99_ns":      latencies[len(latencies)*99/100],
			"mean_ns":     mean(latencies),
			"min_ns":      latencies[0],
			"max_ns":      latencies[len(latencies)-1],
		}
		results = append(results, r)
		fmt.Printf("conc=%-4d p50=%-7dns p99=%-8dns mean=%-7dns min=%-7dns max=%-7dns\n",
			c, r["p50_ns"], r["p99_ns"], r["mean_ns"], r["min_ns"], r["max_ns"])
	}

	out := map[string]any{
		"results":    results,
		"elapsed_ms": time.Since(start).Milliseconds(),
		"note":       "M1 in-process Go channel baseline. NATS/Redis/gossip deferred.",
	}
	f, _ := os.Create(filepath.Join(*outDir, "fit.json"))
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
	fmt.Printf("\nElapsed: %v\n", time.Since(start))
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
