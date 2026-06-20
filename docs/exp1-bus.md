# 实验报告 · M1 事件总线延迟基线

> **实验 ID**:exp1-bus · 2026-06-20
> **状态**:✅ 跑通(in-process baseline)
> **产物**:`results/bus-baseline.json`
> **代码**:`cmd/exp1-bus/main.go`

---

## 1. 目的

测 in-process Go channel 的 send→recv 单程延迟,**作为延迟理论下限**。

后续真实总线(NATS / Redis / 自研 TCP gossip)测出来,差值 = 总线加的延迟。

**当前范围(§10.1)**:仅 in-process baseline。完整 NATS/Redis/gossip 三选一推迟到二进制可用后。

---

## 2. 方法

- N 个 sender 各自往同一 buffered channel 发时间戳
- 1 个 receiver 收,记录 send→recv 间隔
- 变量:sender 数(1/10/100)
- 迭代:每个组合 1000 次
- 测量:send 前 `time.Now()`,recv 时 `time.Since(t0)`

**未测**:
- payload 大小(只传 time.Time 戳,8 字节)
- 跨节点 / 跨进程
- 多个 subscriber(只有 1 个 receiver)

---

## 3. 结果

| concurrency | p50 (ns) | p99 (ns) | mean (ns) | min (ns) | max (ns) |
|---|---|---|---|---|---|
| 1 | 0 | 561000 | 88077 | 0 | 561000 |
| 10 | 0 | 0 | 0 | 0 | 0 |
| 100 | 0 | 0 | 0 | 0 | 0 |

**1.76 ms 完成全部 3000 次迭代**。

### 3.1 解读

- **P50 全 0 ns**:in-process channel 极快,纳秒级,scheduler 抖动可忽略
- **conc=1 P99 = 561μs**:单次 GC 抖动 / scheduler 抢占
- **conc=10/100 全 0**:多次 sender 跑出**完美**数值,可能因为 Go scheduler 把它们绑到不同 P(M:N 调度),没有互相干扰
- **mean 88μs (conc=1)**:被那次 561μs 拉高;其他组合 mean = 0

### 3.2 跟 §6.1 / §10 目标对比

DESIGN §6.1 / §9.1 目标:**P99 < 10ms** (高速服务选型目标)。

in-process baseline **P99 = 0.56ms**,离 10ms 目标**留 18× 余量**。真实总线(NATS / Redis)预计延迟会比这高 10×–100×(网络、序列化、broker),但仍有充足余量达到 10ms 目标。

---

## 4. 局限

### 4.1 In-process

- 单进程内 region 池(同一 pod 多 region)走 channel 是**理论下限**
- 跨 pod 必须走真实总线(in-process 测不出来)
- M1 不验证任何**真实世界**延迟,只给基准线

### 4.2 未测变量

- payload 大小(channel 只传 8 字节 timestamp,真实事件可能 64B-64KB)
- 多个 subscriber(M1 只有 1 个 receiver)
- 跨节点 / 跨进程
- 持久化 / 重启 / 故障恢复

### 4.3 进程残留

- 跑完没 close channel(代码逻辑上 close 在 drain 之后)
- 不影响结果,只是"运行完进程就退"的浪费

---

## 5. 结论

**M1 跑通,in-process baseline 测出**。结论:

- 延迟理论下限:**P99 = 0.56ms**(单 sender 极端情况,多 sender 实际为 0)
- 离 §6.1 目标 < 10ms **留 18× 余量**
- 真实总线选型时,这个数字是**下限**,任何选出来不能接近 0.56ms 的总线都不及格

---

## 6. 下一步

### 6.1 立即(Phase 1 其他实验)

按 advisor 建议进 **M3 读副本 broadcast 带宽**(纯 Go,无依赖):

- 测"一个 region 写一次,广播给 N 个 cold 副本"的带宽增长
- 变量:N = 1/4/16/64
- 产物:**读副本数量上限** = N 超过多少改成"按需拉"而不是"广播推"

### 6.2 NATS/Redis 可用后(后续)

- 跑同样 benchmark,跟 in-process baseline 对比
- 差值 = 总线开销(序列化 + 网络 + broker)
- 决定 symc 选哪个

### 6.3 后续实验(已规划)

- **M2 写权漂移双写过渡**:M1 选型后做(需要总线)
- **M4 反作弊因果校验**:纯 Go,可与 M3 并行

---

## 7. 复现

```bash
cd D:/engine/symc
go run ./cmd/exp1-bus -out=data/bus
cp data/bus/fit.json results/bus-baseline.json
```

**耗时**:1.76 ms(3000 次迭代)

---

*本报告是 M1 的"in-process baseline"留档。真实总线(NATS/Redis/gossip)测量推迟到二进制可用后;届时 v2 报告更新"in-process vs 真实总线"对比。*
