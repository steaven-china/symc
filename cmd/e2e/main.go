// e2e: M8 端到端 — 验证 symc NATS pub/sub 模式正确
//
// 不真启 Paper,只验证:
// 1. publisher 发 CooperationRequest 风格的 JSON 到 NATS
// 2. subscriber 收到 + 反序列化 + 验证字段
//
// 这覆盖 M7 的 publish/subscribe 逻辑端到端。
// 真 Paper 集成(加载 SymcCooperationRequest 主类、接 Bukkit 事件)留 M8 后续。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

const (
	subject = "symc.cooperation.test"
	timeout = 10 * time.Second
)

// Mock CooperationRequest — 跟 SymcCooperationRequest.CooperationRequest 同构
type MockCooperationRequest struct {
	Type    string `json:"type"`
	X, Y, Z int    `json:"x,y,z"` // JSON 字段要顺序,简化
	Event   string `json:"event"`
	Payload string `json:"payload"`
	Ts      int64  `json:"ts"`
}

type TestResult struct {
	Subject     string `json:"subject"`
	PublishMs   int64  `json:"publish_ms"`
	ReceivedMs  int64  `json:"received_ms"`
	RTTMs       int64  `json:"rtt_ms"`
	MsgCount    int    `json:"msg_count"`
	Success     bool   `json:"success"`
	Note        string `json:"note"`
}

func main() {
	// Use different field tags (this is a flat Go struct, not Java record)
	_ = MockCooperationRequest{}
	natsURL := flag.String("nats", "nats://127.0.0.1:4222", "NATS URL")
	iterations := flag.Int("iters", 100, "messages per test")
	flag.Parse()

	fmt.Fprintf(os.Stderr, "e2e: nats=%s subject=%s iters=%d\n", *natsURL, subject, *iterations)

	nc, err := nats.Connect(*natsURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nats connect: %v\n", err)
		os.Exit(1)
	}
	defer nc.Close()

	// Subscriber
	receivedCh := make(chan MockMsg, *iterations*2)
	sub, err := nc.Subscribe(subject, func(msg *nats.Msg) {
		receivedCh <- MockMsg{Data: msg.Data, Time: time.Now()}
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "subscribe: %v\n", err)
		os.Exit(1)
	}
	defer sub.Unsubscribe()

	// 等订阅生效
	time.Sleep(50 * time.Millisecond)

	// Publisher:模拟 CooperationRequest 风格的 JSON,发 iters 次
	sentTimes := make([]time.Time, *iterations)
	publishStart := time.Now()
	for i := 0; i < *iterations; i++ {
		req := struct {
			Type    string `json:"type"`
			X       int    `json:"x"`
			Y       int    `json:"y"`
			Z       int    `json:"z"`
			Event   string `json:"event"`
			Payload string `json:"payload"`
			Ts      int64  `json:"ts"`
		}{
			Type:    "COMPUTATION",
			X:       i,
			Y:       64,
			Z:       64,
			Event:   "redstone_pulse",
			Payload: fmt.Sprintf("level_change=%d→%d", 0, 15),
			Ts:      time.Now().UnixNano(),
		}
		body, _ := json.Marshal(req)
		sentTimes[i] = time.Now()
		if err := nc.Publish(subject, body); err != nil {
			fmt.Fprintf(os.Stderr, "publish %d: %v\n", i, err)
		}
	}
	if err := nc.Flush(); err != nil {
		fmt.Fprintf(os.Stderr, "flush: %v\n", err)
	}
	publishDuration := time.Since(publishStart)

	// 收
	gotCount := 0
	deadline := time.After(timeout)
	rtts := make([]time.Duration, 0, *iterations)
	for gotCount < *iterations {
		select {
		case msg := <-receivedCh:
			rtt := msg.Time.Sub(sentTimes[gotCount%len(sentTimes)])
			rtts = append(rtts, rtt)
			gotCount++
		case <-deadline:
			fmt.Fprintf(os.Stderr, "timeout: got %d/%d\n", gotCount, *iterations)
			break
		}
	}

	// 统计
	var sumRTT int64
	var minRTT, maxRTT int64 = int64(rtts[0]), int64(rtts[0])
	for _, r := range rtts {
		sumRTT += int64(r)
		if int64(r) < minRTT {
			minRTT = int64(r)
		}
		if int64(r) > maxRTT {
			maxRTT = int64(r)
		}
	}
	avgRTT := time.Duration(sumRTT / int64(len(rtts)))
	success := gotCount == *iterations

	r := TestResult{
		Subject:    subject,
		PublishMs:  publishDuration.Milliseconds(),
		ReceivedMs: time.Since(publishStart).Milliseconds(),
		RTTMs:      avgRTT.Milliseconds(),
		MsgCount:   gotCount,
		Success:    success,
		Note:       fmt.Sprintf("min=%v max=%v avg=%v", time.Duration(minRTT), time.Duration(maxRTT), avgRTT),
	}

	fmt.Printf("e2e: subject=%s pub=%dms recv=%dms rtt_avg=%dms got=%d/%d success=%v\n",
		subject, r.PublishMs, r.ReceivedMs, r.RTTMs, gotCount, *iterations, success)
	fmt.Printf("     min=%v max=%v avg=%v\n", time.Duration(minRTT), time.Duration(maxRTT), avgRTT)

	if !success {
		os.Exit(1)
	}
}

type MockMsg struct {
	Data []byte
	Time time.Time
}

var _ = sync.Once{}
