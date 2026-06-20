# 实验报告 · M1 真实 NATS 总线延迟

> **实验 ID**:exp1-bus-nats · 2026-06-20
> **状态**:✅ 跑通
> **产物**:`results/bus-nats-real.json`
> **代码**:`cmd/exp1-bus-nats/main.go`
> **基础设施**:`third-party/nats-data/`(NATS server 2.14.2,JetStream 启用)

---

## 1. 目的

测**真实 NATS pub/sub** 端到端延迟(pub → broker → sub),跟 in-process channel baseline 对比。

**差值 = 总线开销**(序列化 + 网络 + broker)。

---

## 2. 方法

- NATS server 2.14.2 跑在 `127.0.0.1:4222`(本机,无网络)
- 1 个 subscriber + N 个 publisher(串行循环发,模拟并发)
- 每条消息前 8 字节存 `t0` 纳秒戳,subscriber 收到后算 `t1 - t0` 单程延迟
- 变量:payload 大小(64B / 1KB / 64KB)× publisher 数(1 / 10 / 100)
- 迭代:每个组合 1000 次

**跟 in-process baseline 区别**:
- baseline:进程内 channel,无序列化,无网络
- 真实:NATS 协议(`INFO` 握手 + `PUB` 消息 + `MSG` 分发),每条消息有 header overhead

---

## 3. 结果

| payload | conc | P50 (μs) | P99 (μs) | mean (μs) | min (μs) | max (μs) |
|---|---|---|---|---|---|---|
| 64B | 1 | 504 | 874 | 441 | 0 | 874 |
| 64B | 10 | 504 | 1098 | 620 | 504 | 1098 |
| 64B | 100 | 616 | 624 | 315 | 0 | 624 |
| 1KB | 1 | 1036 | 1793 | 858 | 0 | 1793 |
| 1KB | 10 | 712 | 2452 | 1033 | 0 | 2452 |
| 1KB | 100 | 764 | 1309 | 784 | 0 | 1507 |
| 64KB | 1 | 2735 | **31901** | 4555 | 508 | 33987 |
| 64KB | 10 | 3244 | **29108** | 5516 | 0 | 39995 |
| 64KB | 100 | 4186 | **34119** | 7427 | 0 | 41969 |

**全部 9 个组合 2.6s 跑完**。

### 3.1 对比 in-process baseline

| payload | in-process P50 | NATS P50 | 差值(NATS - in-proc) |
|---|---|---|---|
| 64B | 0 | 504μs | **504μs**(总线开销) |
| 1KB | 0 | 1036μs | **1036μs** |
| 64KB | 0 | 2735μs | **2735μs** |

in-process 纳秒级 vs NATS 微秒级,**差 3-4 个数量级**。这是真实的总线开销。

### 3.2 关键发现:64KB P99 严重超标

- **64KB P99 = 31.9ms**(NATS 真实)
- §6.1 / §9.1 目标 = **< 10ms**
- **超出 3.2×** ❌

含义:大 payload + 高 P99 不达标的场景,MC 玩家跨区会**感知到卡顿**。

**实际影响**:
- 64KB ≈ 玩家完整 inventory / 一个 chunk 的 world state snapshot
- MC server 之间真实消息大小通常 64B ~ 1KB(增量 tick state)
- 64KB 是**异常大**的场景(可能:全玩家跨区一次性传 inventory)

---

## 4. 结论

**真实 NATS 总线延迟 = 500μs-30ms**,in-process baseline 0μs + NATS overhead。

### 4.1 推荐(NATS 选型)

- **小型消息(64B-1KB)**:NATS P99 1-2ms,远低于 §6.1 目标 10ms ✅
- **大消息(64KB)**:NATS P99 30ms,超标 3× ❌
- **建议**:大消息**拆分**(64KB → 16 个 4KB,或者 batch 多个小事件合并)
- **或者**:换更高效的总线(gRPC + protobuf? 自研 TCP?)

### 4.2 §6.1 选型决策(基于此实验)

NATS JetStream **作为基线 OK**,但:
- 64KB 单消息 → 拆
- 1MB+ 大数据 → 不要走 NATS,用对象存储 + manifest 引用
- 64B-4KB 热路径 → NATS 极佳

### 4.3 进一步测试(后续)

- **Redis Streams** 同 benchmark,对比 NATS
- **自研 TCP gossip** 同 benchmark(理论上 < 500μs)
- **跨节点** NATS:同 cluster 不同机器,看网络延迟
- **100K+ events/s** 压测:NATS 官方标称 1M+ msgs/s,验证

---

## 5. 复现

```bash
# 1. 启动 NATS server
mkdir -p third-party/nats-data
"D:/go/bin/nats-server.exe" -p 4222 -js -sd D:/engine/symc/third-party/nats-data -l D:/engine/symc/third-party/nats.log &

# 2. 跑实验
cd D:/engine/symc
go run ./cmd/exp1-bus-nats -out=data/bus-nats
cp data/bus-nats/fit.json results/bus-nats-real.json
```

**耗时**:2.6 秒(9 个组合)

---

*本报告是 M1 真实 NATS 测量的留档。NATS/Redis/gossip 选型决策详见 §6.1;后续补 Redis 同 benchmark + 跨节点延迟。*
