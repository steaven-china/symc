// exp4-anticheat: M4 反作弊因果校验开销(2026-06-20,minimal 修订)
//
// 测 CausalityHash = SHA-256(EventID || OriginRegion || Tick || Payload) 的 hash + 验证开销。
// 简化:只测 hash_len=8B(已推荐,详见 docs/exp4-anticheat.md)。
// 测量方式:batch 计时(每 batchSize 次 hash 一次 time.Now),绕开 Windows 精度。
package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	hashLen    = 8
	batchSize  = 100
	duration   = 1 * time.Second
	eventCount = 5000 // 固定事件数(不靠 ticker)
)

type Result struct {
	HashLen       int   `json:"hash_len"`
	Events        int   `json:"events"`
	HashTimeNs    int64 `json:"total_hash_time_ns"`
	ValidateNs    int64 `json:"total_validate_time_ns"`
	HashAvgNs     int64 `json:"hash_avg_ns_per_event"`
	ValidateAvgNs int64 `json:"validate_avg_ns_per_event"`
	FailedValid   int64 `json:"failed_validations"`
}

func main() {
	outDir := flag.String("out", "data/anticheat", "")
	flag.Parse()
	start := time.Now()
	_ = os.MkdirAll(*outDir, 0o755)

	payload := make([]byte, 256)
	for i := range payload {
		payload[i] = byte(i)
	}

	// 1. 测 hash 开销
	var hashTimeNs int64
	for batch := 0; batch < eventCount/batchSize; batch++ {
		batchStart := time.Now()
		for i := 0; i < batchSize; i++ {
			eventID := uint64(batch*batchSize + i)
			_ = computeHash(eventID, "region-A", eventID, payload)[:hashLen]
		}
		hashTimeNs += time.Since(batchStart).Nanoseconds()
	}

	// 2. 测验证开销(重算 + 比对)
	var validateTimeNs int64
	var failed int64
	for batch := 0; batch < eventCount/batchSize; batch++ {
		batchStart := time.Now()
		for i := 0; i < batchSize; i++ {
			eventID := uint64(batch*batchSize + i)
			h := computeHash(eventID, "region-A", eventID, payload)[:hashLen]
			expected := computeHash(eventID, "region-A", eventID, payload)[:hashLen]
			if !bytesEq(h, expected) {
				failed++
			}
		}
		validateTimeNs += time.Since(batchStart).Nanoseconds()
	}

	totalEvents := int64(eventCount)
	hashAvg := hashTimeNs / totalEvents
	validAvg := validateTimeNs / totalEvents

	r := Result{
		HashLen:       hashLen,
		Events:        eventCount,
		HashTimeNs:    hashTimeNs,
		ValidateNs:    validateTimeNs,
		HashAvgNs:     hashAvg,
		ValidateAvgNs: validAvg,
		FailedValid:   failed,
	}
	fmt.Printf("hash_len=%d events=%d hash_avg_ns=%d validate_avg_ns=%d failed=%d\n",
		hashLen, eventCount, hashAvg, validAvg, failed)

	out := map[string]any{
		"results":    []Result{r},
		"elapsed_ms": time.Since(start).Milliseconds(),
		"note":       "M4 minimal: batch timing, hash_len=8 only. Full sweep deferred.",
	}
	f, _ := os.Create(filepath.Join(*outDir, "fit.json"))
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
	fmt.Printf("Elapsed: %v\n", time.Since(start))
}

func computeHash(eventID uint64, originRegion string, tick uint64, payload []byte) []byte {
	h := sha256.New()
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], eventID)
	h.Write(buf[:])
	h.Write([]byte(originRegion))
	binary.LittleEndian.PutUint64(buf[:], tick)
	h.Write(buf[:])
	h.Write(payload)
	return h.Sum(nil)
}

func bytesEq(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
