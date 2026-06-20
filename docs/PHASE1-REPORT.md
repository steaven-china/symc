# Phase 1 完整报告

> **日期**:2026-06-20
> **状态**:Phase 0 + Phase 1 主实验 + Phase 3 文档回填 + P4 缓冲带 完成
> **范围**:M5.5 / M1 / M3 / M4 / P4 实验,DESIGN.md / DECISIONS.md 文档闭环

---

## 1. 整体概览

本 phase 目标是**为 symc 分布式 MC 同步引擎建立"实验 + 文档"闭环**。从 6 月 20 日早上的纯设计稿,到当天结束时的 6 个 in-process baseline 实验 + 1 个真实 NATS benchmark + 完整文档。

**关键转变**:
- 从"概念设计" → "实验验证"
- 从"参数占位" → "实测数字"
- 从"决策清单" → "已定方案 + advisor review 闭环"

---

## 2. 实验清单(全部跑通)

| 实验 | 报告 | 结果 | 关键发现 |
|---|---|---|---|
| **M5.5 温度场 sim** | `docs/exp-tempfield.md` | `results/tempfield-fit.json` | 公式实现可跑;合成数据 trivial fit(限已知) |
| **M1 in-process channel** | `docs/exp1-bus.md` | `results/bus-baseline.json` | P99=0.56ms(单 sender);§6.1 < 10ms 目标留 18× 余量 |
| **M1 真实 NATS** | `docs/exp1-bus-nats.md` | `results/bus-nats-real.json` | 64B-1KB P99 < 2ms ✅;**64KB P99=31.9ms ❌** 超过目标 3.2× |
| **M3 读副本 broadcast** | `docs/exp3-replica.md` | `results/replica-baseline.json` | 6.7M msgs/s(replicas=64);推荐读副本 ≤16 |
| **M4 反作弊因果校验** | `docs/exp4-anticheat.md` | `results/anticheat-fit.json` | hash 219ns/event;推荐 8B 截断 |
| **P4 缓冲带 sim** | `docs/exp-bufferzone.md` | `results/bufferzone-fit.json` | N=1 性价比拐点(76% 减 event, +25% 算力) |

**留档**:
- 6 份实验报告(`docs/exp*.md`)
- 6 份结果 JSON(`results/*.json`)
- 6 个 Go sim 代码(`cmd/exp*/`)
- 1 份 Phase 0 基线文档(`docs/phase0-baseline.md`)

---

## 3. 文档闭环(DESIGN.md 回填)

### 3.1 §6.1 高速服务选型

- in-process baseline 数字 ✅
- 真实 NATS 数字 ✅(含 64KB 警告)
- 三种候选保留(NATS / Redis / gossip),Redis/gossip 待跑

### 3.2 §10.1 / §10.3 / §10.4 产物行

- M1 / M3 / M4 实测数字已嵌入产物行
- 各组合的具体数字详见 §6.1 / 各 docs/exp*.md

### 3.3 §10.5 反过拟合约定

- M5.5 应用记录 ✅
- Train/Val gap 0.66%(synthetic data trivial fit)
- 等真实数据替代合成数据后重跑

### 3.4 §5.2.1 缓冲带

- P4 实测:默认 N=1(76% 减 event, +25% 算力)
- N=2 适合"必须零跨区"场景(+50% 算力)

### 3.5 §11 风险表

- 缓冲带算力税条目更新(P4 数据:N=1 +25%, N=2 +50%, N=4 +100%)

---

## 4. 关键发现(按价值排序)

### 4.1 **64KB 大消息 P99 超标 3.2×**

- 真实 NATS 测出:64KB P99 = 31.9ms(目标 10ms)
- **行动**:MC server 实际消息 64B-4KB 范围(< 1KB 最常见),64KB 异常;**需拆分或换总线**
- **影响**:影响 §5.6 协议字段大小设计(避免单条 CompositeEvent > 4KB)

### 4.2 **缓冲带 N=1 性价比拐点**

- N=0 → 10 events,N=1 → 2.4 events,N=2+ → 0 events
- N=1:76% event 减少,只 +25% 算力税
- **行动**:K8s controller 默认 N=1,玩家密度高峰开 N=2

### 4.3 **in-process channel 延迟纳秒级**

- in-process baseline P50=0ns,P99=0.56ms
- 真实 NATS P50=504μs(64B) → 4.2ms(64KB)
- **差值 = 总线开销,500μs-4ms 数量级**

### 4.4 **CausalityHash 8B 截断足够**

- SHA-256 前 8/16/32B 截断,hash 开销 219ns/event
- 8B 截断碰撞概率 ~100K events/s 下 << 1%
- **行动**:DESIGN.md §5.4 / §8.3 CompositeEvent 字段长度用 8B

### 4.5 **M5.5 合成数据 trivial fit**

- TrueWeights 已知 → 网格搜索找到同解
- **不**提供真实校准价值(初值 w₁=w₂=w₃=1.0 仍是默认)
- **等真实 MC 玩家行为数据后重跑**

---

## 5. M2 写权漂移双写过渡 — 未跑

**原因**:依赖真实总线选型(NATS / Redis / gossip)确定才能跑 in-distributed 场景。

**当前状态**:
- §5.2 / §5.6 写权漂移流程已设计完成
- M5.5 温度场 sim 已跑(给 M6 写权漂移集成梯度预测做准备)
- P4 缓冲带 sim 已跑(给 M2 双写窗口玩家感知阈值提供 baseline)

**预计时间**:
- 跑 NATS 真实 M2:1 周(代码 200 行 + benchmark)
- 跑 Redis 真实 M2:同上,1 周
- 选型:再 1 周(对比 NATS / Redis 数字)
- 总:3 周

---

## 6. M5 / M6 / M7 / M8 / M9 — 未开始

依赖 JDK 25 + Paper fork 准备,1-2 天(P2):

| Milestone | 状态 | 依赖 |
|---|---|---|
| M5 Paper fork 准备 | ⏸ | JDK 25 装好 |
| M5.5 温度场 sim 跑通 | ✅(已提前) | — |
| M6 Paper 集成 | ⏸ | M5 |
| M7 K8s controller | ⏸ | M6 |
| M8 端到端集成测试 | ⏸ | M7 |
| M9 性能调优 + advisor 复审 | ⏸ | M8 |

---

## 7. 数据 + src 留档完整性

### 7.1 实验数据(可复现)

```
D:/engine/symc/
├── docs/
│   ├── phase0-baseline.md           # Phase 0 完整基线
│   ├── exp-tempfield.md             # M5.5 报告
│   ├── exp1-bus.md                  # M1 in-process 报告
│   ├── exp1-bus-nats.md             # M1 真实 NATS 报告
│   ├── exp3-replica.md              # M3 报告
│   ├── exp4-anticheat.md            # M4 报告
│   ├── exp-bufferzone.md            # P4 报告
│   └── PHASE1-REPORT.md             # 本文档
├── results/
│   ├── tempfield-fit.json           # M5.5 拟合结果
│   ├── bus-baseline.json            # M1 in-process baseline
│   ├── bus-nats-real.json           # M1 真实 NATS
│   ├── replica-baseline.json        # M3 baseline
│   ├── anticheat-fit.json           # M4 拟合结果
│   └── bufferzone-fit.json          # P4 拟合结果
├── cmd/
│   ├── sim/                          # 入口(M0)
│   ├── exp-tempfield/                # M5.5
│   ├── exp1-bus/                     # M1 in-process
│   ├── exp1-bus-nats/                # M1 真实 NATS
│   ├── exp3-replica/                 # M3
│   ├── exp4-anticheat/               # M4
│   └── exp-bufferzone/               # P4
├── pkg/layer/
│   ├── layer.go                      # 4 层分类(基础)
│   └── tempfield.go                  # §3.2 温度场实现
├── Paper/                            # Paper 26.1.2 fork(浅克隆,未动)
├── third-party/
│   ├── nats-data/                    # NATS JetStream 持久化
│   └── nats.log                      # NATS server 日志
├── DESIGN.md                         # 1262 行,4 处 P3/P4 回填
├── DECISIONS.md                      # 282 行,决策记录
├── README.md                         # 675 行,讨论稿
└── WATCHDOG.md                       # 79 行,advisor 指引
```

### 7.2 可复现命令

```bash
# 启动 NATS(后台)
"D:/go/bin/nats-server.exe" -p 4222 -js -sd D:/engine/symc/third-party/nats-data -l D:/engine/symc/third-party/nats.log &

# M5.5 温度场
go run ./cmd/exp-tempfield -seed=42

# M1 in-process
go run ./cmd/exp1-bus

# M1 真实 NATS
go run ./cmd/exp1-bus-nats

# M3 读副本
go run ./cmd/exp3-replica

# M4 反作弊
go run ./cmd/exp4-anticheat

# P4 缓冲带
go run ./cmd/exp-bufferzone
```

---

## 8. 已知遗留

### 8.1 真实数据缺失

- M5.5 温度场 sim 用了 TrueWeights 已知 → trivial fit
- 真实 MC 玩家行为 / 红石频率 / 实体密度数据需从 Paper 实装后采
- 在 Paper 集成前,所有"参数校准"都是占位

### 8.2 总线选型不完整

- 跑过 NATS,Redis / gossip 未跑
- §6.1 决策:继续跑 Redis(下一轮),或确定 NATS 够用,跳 gossip
- **推荐**:Redis benchmark 跑一下,看是否 NATS 全面胜出(避免单源依赖)

### 8.3 跨节点延迟未测

- 所有 in-process baseline + NATS 测的是**同节点 localhost**
- K8s 多 pod 跨节点的延迟未测(需要第二个节点或 K8s cluster)
- 这是 M8 集成测试要解决的

### 8.4 M2 / M5 / M6 / M7 / M8 / M9 未跑

- M2 等总线选型(预计 3 周)
- M5+ 等 JDK 25 装好(预计 1-2 天)

---

## 9. 接下来的优先级

| 优先级 | 任务 | 预计时间 |
|---|---|---|
| 1 | P2 装 JDK 25 + Paper fork 准备 | 1-2 天 |
| 2 | Redis Streams benchmark(补选型) | 1 天 |
| 3 | M2 写权漂移(NATS 真实版) | 1 周 |
| 4 | M5.5 真实数据重跑(等 M6 Paper 集成后) | — |

---

## 10. 致谢

- **advisor**(deepseek-v4-pro):5 轮 review,抓出 8 个数学/前提/结构问题,提供 dual gate + 写权漂移双写过渡等关键 insight
- **user**(Steaven Jiang):导向 + 反馈"我对你手贱" + 提供代理 IP,容忍我多次 build 失败
- 各种 build 失败教训:Edit SWAP 范围陷阱 / 行号漂移 / advisor hallucinate 自查

---

*本报告是 2026-06-20 一日 symc 项目从"概念设计"到"实验验证"的总结。*
*下一步:继续往 M2 + Paper fork 实装方向推进。*
