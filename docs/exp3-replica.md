# 实验报告 · M3 读副本 broadcast 带宽

> **实验 ID**:exp3-replica · 2026-06-20
> **状态**:✅ 跑通(in-process baseline)
> **产物**:`results/replica-baseline.json`
> **代码**:`cmd/exp3-replica/main.go`

---

## 1. 目的

测"一个 primary region 写一次,广播给 N 个 cold 副本"的带宽增长。

**目标**:给出**读副本数量上限** = N 超过多少改成"按需拉"而不是"广播推"。

---

## 2. 方法

- 1 个 primary goroutine 持续写 payload
- N 个 replica goroutine 各自通过独立 buffered channel 收
- 测:在固定 duration(2s)内能 broadcast 多少 bytes(总带宽 = N × payload × msg_count)
- 变量:payload 大小(64B / 1KB / 64KB) × replica 数(1/4/16/64)

**注**:这是**in-process** 内存 copy 带宽,真实网络(跨 pod)会差 10-100×。

---

## 3. 结果

| payload | replicas | msgs | bandwidth | msgs/sec |
|---|---|---|---|---|
| 64 B | 1 | 27,694,689 | 845 MB/s | 13.8M |
| 64 B | 4 | 18,724,360 | 571 MB/s | 9.4M |
| 64 B | 16 | 17,208,160 | 525 MB/s | 8.6M |
| 64 B | 64 | 13,493,504 | 412 MB/s | 6.7M |
| 1 KB | 1 | 25,481,405 | 12.4 GB/s | 12.7M |
| 1 KB | 4 | 16,324,072 | 8.0 GB/s | 8.2M |
| 1 KB | 16 | 14,905,856 | 7.3 GB/s | 7.5M |
| 1 KB | 64 | 13,511,040 | 6.6 GB/s | 6.8M |
| 64 KB | 1 | 25,552,552 | 798 GB/s | 12.8M |
| 64 KB | 4 | 15,758,668 | 492 GB/s | 7.9M |
| 64 KB | 16 | 14,955,968 | 467 GB/s | 7.5M |
| 64 KB | 64 | 13,609,152 | 425 GB/s | 6.8M |

**全部 12 个组合 24 秒跑完**。

### 3.1 关键观察

- **payload 越大,bandwidth 越高**:64B 几百 MB/s,64KB 几百 GB/s —— 受 L3 cache / 内存带宽限制
- **replicas 越多,单 replica 收到越少**:
  - 64B:replicas=1 时 13.8M msgs/s,replicas=64 时 6.7M msgs/s(单 replica 收)—— primary 总发送数也下降了(因为 broadcast 本身受 primary 速度限制)
  - 64KB:replicas=1 时 12.8M msgs/s,replicas=64 时 6.8M msgs/s —— 趋势一致
- **replicas=1 是 single subscriber 性能**;**replicas=64 是 broadcast 极限**
- **replicas 4→16→64 下降缓慢**:primary 不被 broadcast 拖死,说明 in-process 多 channel 分发是**O(N) 但常数很小**

### 3.2 跟 §6.1 / §9 目标对比

DESIGN §6.1 没明确广播带宽目标,但 §5.5 / §11 隐含"广播要可承受"。

in-process 6.6 GB/s 跨 pod 真实场景会变 ~100MB/s(LAN)或 ~10MB/s(WAN)——可承受。

---

## 4. 局限

### 4.1 In-process

- 内存 copy 带宽 = 上限,不是真实场景
- 跨 pod 走真实网络,带宽差 10-100×,延迟也涨
- 这个实验**不**测真实广播延迟

### 4.2 只测 2 秒

- 短窗口测,可能没考虑 GC / scheduler 抖动稳态
- 长跑可能带宽下降 30%+
- **建议**:真实评估跑 30 秒以上

### 4.3 单进程

- 没测多进程 / 多 pod 协同
- K8s controller 调度、anti-affinity、PV 持久化对广播的间接影响都没测

---

## 5. 结论

**M3 跑通,in-process broadcast 测出**。结论:

- **单 subscriber 性能** (replicas=1):**12-13M msgs/s**
- **broadcast 极限** (replicas=64):**6.7M msgs/s**(主 primary 不被拖死)
- **payload 64B → 64KB**:带宽 0.8 GB/s → 800 GB/s(受内存限制)
- 跨 pod 真实场景预计下降 10-100×,仍可承受

### 5.1 读副本数量上限(粗)

in-process 几乎无上限,真实网络下需要按带宽预算算:

- 假设 1 Gbps LAN(125 MB/s),replica=1 跑 125MB/s ÷ 1KB/msg = 125K msgs/s
- broadcast 给 N 个 replica:primary 总带宽 = N × 1KB × msgs/s
- msgs/s 上限 = 125M / N
- N=64 时,msgs/s ≈ 2M —— 仍够
- **结论**:LAN 1Gbps 至少 N=64 可承受;10Gbps 至少 N=640 可承受;真实数字等 NATS 测出来

---

## 6. 下一步

### 6.1 立即(Phase 1 其他实验)

按 advisor 建议进 **M4 反作弊因果校验开销**(纯 Go,无依赖):

- 测 CompositeEvent 加 OriginRegion/OriginTick/CausalityHash 后的带宽 + CPU 开销
- 变量:CausalityHash 长度 8B / 16B / 32B × 事件频率 100 / 1000 / 10000 / tick
- 产物:**带宽成本数字 + 推荐的 CausalityHash 长度**

### 6.2 NATS/Redis 可用后

- 同样 benchmark 跑跨 pod
- in-process baseline 当对照
- 决定 symc broadcast 走 NATS pub/sub 还是 Redis streams

---

## 7. 复现

```bash
cd D:/engine/symc
go run ./cmd/exp3-replica -out=data/replica
cp data/replica/fit.json results/replica-baseline.json
```

**耗时**:24 秒(12 个组合 × 2 秒)

---

*本报告是 M3 的"in-process broadcast 带宽"留档。真实网络(NATS/Redis)测量推迟到二进制可用后;届时 v2 报告更新"in-process vs 真实网络"对比。*
