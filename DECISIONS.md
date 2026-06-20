# Open Decisions — symc 5 个待拍板的设计问题

> 给 advisor 用的上下文(README 太大,advisor 不会全读)。
> README 全文: `README.md`
> 已定决策 / 自推方案(8 个): 在 README §9、§12 里,以及上一轮对话里我自己推的(我先不写进 README,等 advisor 拍板再合并)。
> 当前这篇只列**待拍板**的 5 个 — 这些是真正影响架构走向的,不应我一个人定。

---

## 项目背景(简)

- 目标:分布式 MC 同步引擎,世界切 region 跑,玩家跨区无感
- 不从零写,基于 Paper fork
- 核心抽象: `Cell = (chunk, tick, time_window)` 原子单元,事件按 Layer 分级,按 Weight 竞争资源,跨 region 走 CompositeEvent + CooperationRequest
- MVP:先 standalone Go simulator,跑出指标再接 Paper
- 仓库: `D:/engine/symc/`
  - `README.md` — 讨论稿(675 行,11 个主章节)
  - `Paper/` — 浅克隆的 PaperMC/Paper `ver/26.1.2` 分支(没 build,本机 JDK 21 < 25)
  - `cmd/sim/`, `pkg/{cell,layer,weight,...}` — Go sim 骨架,能跑 hello
  - `.gradle-env.ps1` — 隔离 Gradle 缓存到 D 盘,防 C 盘爆
  - `go.mod` — `github.com/symc/sim`

---

## D1. Weight 公式的 8 个参数权重(对应 README §9.2 / §11.4)

> **advisor 2026-06-20 review**:纯乘法 + 加减混合 → 公式结构根本不对。8 个因子强行加权调不出来。改用"门控 + 精排"两段:Load/NetworkCost/Consistency 做门控规则(不进分数),剩下 3 个变量进 tanh 归一化。BatchGain/SplitPenalty 改二元判断(能合就合、不能合单独发)。负数 W 在调度里没语义,删掉。

**D1 已定方案(2026-06-20,advisor review 后)**:

**阶段 1:门控(不进 W,做规则)**

| 因子 | 门控规则 |
|---|---|
| Load | `Load > 0.8` → cell 强制降级到 Semi-static,本 tick 不进精排 |
| NetworkCost | `NetworkCost > 0.5` → cell 进 Batch 队列,等下个 tick,本 tick 不进精排 |
| Consistency | 离散档位,不当分数:`strict`(0ms 延迟容忍) / `eventual`(100ms) / `best_effort`(可丢) |

**阶段 2:精排(进了分数,压在 [0,1])**

```
W = tanh( (Urgency × Impact × RegionFactor) × α )
RegionFactor = 1 + 0.3 × (RegionCount - 1)
α = 1.0  // 全局标量,先写死,sim 出数据再调
```

**BatchGain / SplitPenalty**:不进 W,做二元判断 —— 能合到现有 packet → 合;不能合 → 单独走 fast lane。

**调度解释**:

| W 范围 | 处理 |
|---|---|
| > 0.7 | fast sync,触发 tick 对齐 |
| 0.4 ~ 0.7 | 主 region 权威,其他 region 按需接收 |
| 0.1 ~ 0.4 | 允许延迟,Batch 队列 |
| < 0.1 | 仅 Ephemeral 允许丢弃,其他降 Semi-static |

**反思**:8 个因子强行加权是控制论/调度论套到离散分类上的错觉。MC 事件本质离散(4 层 Layer),用连续标量拟合只会出来调不动的公式。结构对了,3 个真实变量就够;不对,8 个也是玄学。

---

## D2. 边界 chunk 双重缓冲实现(README §9.3)

**D2 已定方案 v3(2026-06-20,用户 + advisor 两轮 review 后)**:**单写权 + cold/hot 副本 + 双写过渡**。

**副本分三种状态**:

| 状态 | 含义 | 模拟? | 写权? |
|---|---|---|---|
| **primary** | 当前模拟 chunk,有写权 | 是 | 是 |
| **hot 副本** | 在模拟,但还没拿写权(双写窗口期) | 是 | 否 |
| **cold 副本** | 只存数据,零模拟开销(玩家视锥外) | 否 | 否 |

**为什么不能简单"读副本"**:
MC 的"加载 chunk"会触发实体 tick / 随机 tick / 红石更新,这些都是写。所以"只读副本"在玩家视锥进入时必须变成 hot(模拟 = 写),要么拿写权要么接受 stale。

**写权漂移(常态操作,不是例外)**:
用户原话:"region 是可拼接和动态范围化的,容器非常动态化"——意思是 region 写权在 pod 之间自由漂移,由调度器决定谁持有。Pod 是计算资源,region 是逻辑状态,两者解耦。

**漂移过程(双写过渡,不硬切)**:
1. 漂移开始:目标 pod 把 cold 副本提升为 hot 副本,开始模拟
2. 老 pod 继续模拟(它还是 primary)
3. 时钟对齐 + 状态同步:两方结果按共享时钟基线 merge
4. 收敛:老 pod 释放写权,新 pod 拿写权(变 primary),老 pod 的 hot 副本可选降级为 cold
5. 漂移完成:1 primary + 1 hot(原 primary,可选降级)+ N cold

**这跟 D2 的单写权不矛盾**:双写是转移手段,最终收敛到单写权。"写权"是逻辑概念,不是物理绑定。

**advisor 抓的坑**:硬切换(老 pod 立即停,新 pod 启动)中间 chunk 处于"无人模拟"状态,50ms 玩家就感知到(实体冻结、红石停摆)。**解法是双写 + 时钟对齐**,不是"让窗口更短"。

**与 D3 的关系**:玩家跨区 = transfer packet(D3) + cold→hot 双写过渡(本节)。两个机制都要。

**与 §12 CooperationRequest 协议的关系**:加新请求类型"写权转移"——cold/hot region 申请拿写权,老 primary 放写权,带 CausalityHash(转移前最终状态)。

**与 k8s 部署的关系**:
- Pod 是**通用计算资源**,不是 region 物理载体
- Region 写权通过自定义 controller 分配(类似 Kubernetes Operator + custom resource)
- K8s 只管 pod 生命周期;region 写权漂移由 controller 决定

---

## D3. 玩家跨区连接切换(README §9.4 / §9.7)

**D3 已定方案(2026-06-20,用户拍板)**:方案 A —— proxy(Velocity)知道 region 拓扑,每个 region 是独立 Paper 实例,玩家跨区时主 region 调 `PaperCommonConnection#transfer(host, port)`,客户端走原生 `ClientboundTransferPacket` 流程(不踢、不重连、状态带过去)。

**实现要点**:

- 每个 region 是独立 Paper 进程,`accepts-transfers=true`
- Velocity 作 proxy,知道 region ↔ chunk range 映射(从中央 scheduler 同步)
- 玩家跨区判定在主 region 端(玩家坐标跨越 region 边界),判定后调 `transfer()` 发到目标 region
- 目标 region 的 `AsyncPlayerPreLoginEvent.getTransferred() = true` → 走 transfer 初始化路径(继承玩家状态)

**还差的(下一步定)**:跨区时玩家状态的同步——背包、效果、坐骑、速度向量的原子性。玩家实体本身是跨区 state 容器,需要单独定传输格式。倾向:走 CompositeEvent(玩家跨区事件)发,目标 region 接收后还原。

**与 D2 v3 联动**(advisor 2026-06-20 加的交叉引用):玩家跨区 = transfer packet(D3) + cold→hot 双写过渡(D2 v3)。两个机制都要。玩家视锥进入新 region 时,新 region 通过 `write_authority_transfer` 协议从 cold 副本升级为 hot(双写),同时 transfer packet 把玩家连接切到新 region 进程。
---

## D4. 反作弊基调(README §9.8)

**D4 已定方案(2026-06-20,advisor review 后,用户拍板)**:选项 2 + 选项 4 组合,分层不互斥。

**架构职责拆分**:

| 层 | 答的问题 | 方案 |
|---|---|---|
| **基线:谁说了算** | chunk 物理的最终权威 | 选项 2 — 主 region 对自己 chunk 物理有最终权威,跟 vanilla 一致;邻居 region 偶尔抽查(随机抽样 + 异常触发) |
| **覆盖层:怎么发现跨区作弊** | 跨区作弊窗口的可观测性 | 选项 4 — CompositeEvent 带时间戳 + 因果元数据,架构暴露异常事件给插件订阅 |
| **运营层:怎么处置** | 检测到异常后怎么办 | 不归架构管,交给反作弊插件(ban / kick / log) |

**选项 1 排除**:全联署 = 每个跨区操作等 2×RTT,玩家感知到卡顿,MC 这种交互延迟敏感场景不可行。

**选项 3 排除**:现有反作弊插件(Vulcan/Grim/Spartan)工作在**单服假设**上,跨区作弊窗口它们看不见。放弃架构责任 = 跨区作弊零覆盖。

**具体机制:CompositeEvent 三个字段**:

| 字段 | 类型 | 含义 |
|---|---|---|
| `OriginRegion` | region ID | 事件发起的 region |
| `OriginTick` | tick 序号 | 发起时的 tick(发起 region 的时钟) |
| `CausalityHash` | 哈希值 | 该事件依赖的前置状态的指纹 |

**接收 region 校验(O(1) 哈希比对,不增加延迟)**:

1. `OriginTick` 是否在已知 causal history 内?(不知道的 tick → 异常)
2. `CausalityHash` 是否匹配当前已知状态?(不匹配 → 异常)

任一不通过 → 触发 `AntiCheatAnomalyEvent`,插件订阅决定 ban / kick / log。

**跟其他决策的耦合**:

- 跟 D2 v3(写权漂移 + cold/hot):主 region 对自己 chunk 物理有最终权威 = vanilla
- 跟 D3(玩家跨区走 transfer):玩家状态转移本身原子,不需要反作弊额外处理
- 跟 D1(门控 + 精排):异常事件 W > 0.7(玩家 PvP 边界)走 fast sync,合法玩家不被误判
- 跟 D-extra(CooperationRequest):CooperationRequest 自带 OriginRegion/OriginTick/EventID,CausalityHash 是自然延伸

---

## D5. 多 region 保存/加载协同(README §9.9)

**D5 已定方案(2026-06-20,advisor 2026-06-20 review 增量)**:Anvil 1:1 + 并行存 + manifest commit + **replica_pods 字段**。

**manifest 字段**:

| 字段 | 类型 | 含义 |
|---|---|---|
| `region_id` | string | 唯一 ID |
| `chunk_range` | (x, z, dx, dz) | 区域 chunk 范围(动态范围化) |
| `mtime` | int64 | 上次 commit 时间 |
| `version` | int64 | manifest 版本号(commit 递增) |
| `replica_pods` | [string] | **advisor 2026-06-20 加的**:哪些 pod 持有这个 region 的 cold 副本(用来追踪 sync 拓扑 + 写权漂移候选) |

**流程**:
1. 各 region 并行写自己的 `.mca` 区域文件
2. 写完更新 manifest 中自己的 entry
3. commit = manifest 写盘 + fsync,version++
4. `replica_pods` 在 cold 副本创建/释放时维护(由 controller 写入)
5. 写权漂移时:候选 pod 从 `replica_pods` 里选(优先选 hot 副本,其次 cold 副本升级)

**读时一致性**:加载世界时读 manifest,按 region 列表拉对应 pod。某 region 缺失 = 部分世界加载(MC 已处理)。`replica_pods` 让 pod 故障时能快速找到替代副本。

**D2 v3 联动**:`replica_pods` 是 D2 cold/hot/primary 三态的物理追踪;写权漂移(Cold→hot→primary)全在 `replica_pods` 维护上做。

---

## D-extra. CooperationRequest 协议细节(§12 整章 + §12.9)

**D-extra 已定方案 v2(2026-06-20,D2 v3 联动 + advisor review)**:

**CooperationRequest 协议类型清单**:

| 类型 | 用途 | 触发方 | 接收方 | 时延要求 |
|---|---|---|---|---|
| `computation` | 跨区运算协作(§12.4) | dynamic region | static-mutable region | 中(可等) |
| `state_query` | 读副本查询(只读,不申请写权) | 任何 region | 任何 region | 低 |
| **`write_authority_transfer`**(advisor 2026-06-20 落地) | 写权漂移 — cold/hot region 申请拿写权,老 primary 放写权 | cold/hot region | 当前 primary | **低**(玩家视锥进入时触发) |

**`write_authority_transfer` 协议字段**:
- `request`: `{from_region, to_region, chunk_range, current_tick, target_state_hash}`
- `response`: `{accepted, final_state_hash, transfer_tick, causality_hash}` — final_state_hash 是老 primary commit 完当前 tick 的状态指纹;causality_hash 跟 D4 一致

**流程(双写过渡版,不是硬切)**:
1. cold region 收到"玩家视锥进入"信号 → 发 `write_authority_transfer` 请求
2. 老 primary 继续模拟到下个 tick boundary
3. 老 primary 发 `accepted` + `final_state_hash`
4. cold → hot(开始模拟,跟老 primary 同步跑)
5. 双方按共享时钟基线 merge,直到 next state hash 一致
6. 老 primary 发 `released` → 目标 region 变 primary,老 primary 降级为 hot(可选降 cold)
7. 漂移完成,`replica_pods` 同步更新

**限流**(原 §13.6):写权转移请求按 chunk 范围做限流 — 同一 chunk 范围每秒最多 1 次漂移,避免抖动。**比 §13.6 "每 region 对 20/s" 更严**,因为写权漂移比普通 computation 贵。
---

## 实验与下一步(2026-06-20,用户要求"服务测算和性能估算还有最小化实验")

四个最小化实验,对应"k8s + JWT + 单容器 + 高速服务 + 写权漂移"这个新架构。

### 实验 1:事件总线延迟基线

- **目标**:测三种高速服务实现的 P50/P99 延迟,作为后面所有设计的基线
- **候选**:
  - NATS JetStream(pub/sub + 持久化)
  - Redis Streams(低延迟队列)
  - 自研 TCP gossip(极低延迟,无持久化)
- **变量**:
  - 事件大小:64B / 1KB / 64KB
  - 并发订阅:1 / 10 / 100
  - 拓扑:同节点 / 跨节点
- **产物**:**基线数字 + 选型推荐**

### 实验 2(改):写权漂移的双写过渡

D2 v3 改完了,这个实验替代之前的"region sync 耗时"。

- **核心测量**:
  1. **双写窗口长度**(漂移开始到收敛,多少 tick / 多少 ms)
  2. **双写期间冲突率**(两个 pod 模拟同一 chunk 产生多少状态分歧)
  3. **双写期间冲突类型分布**(advisor 2026-06-20 加的):
     - **可自动合并**:双方操作不互斥(都在不同方块,或都放置/破坏不同位置)→ LWW / set union
     - **需仲裁**:操作互斥但都"合法"(玩家 A 打破方块 vs 玩家 B 放置同一坐标)→ 走仲裁协议(选先到者 / 选确定者)
     - **不可合并**:操作互斥且语义冲突(玩家 A 把自己传送到 chunk X,玩家 B 同时把 A 拉回主世界)→ 标记异常,可能要回滚 + 触发反作弊事件
     各自占比决定冲突消解策略是"自动合并就够了"还是"必须引入仲裁协议"
  4. **时钟对齐误差**(两个 pod 跑同一 tick 起始时间差)
  5. **玩家感知阈值**:双写窗口 < 多少 ms 玩家不卡?
- **对比**:
  - 硬切换(老 pod 立即停,新 pod 启动)— 基线
  - 双写过渡 + 时钟对齐 — 真实方案
- **变量**:
  - chunk 大小:16×16 / 32×32 / 64×64
- 区域活跃度:玩家数 / 红石密度(advisor 2026-06-20 合并实验 4:加 1000 玩家场景,测"漂移时玩家感知")
  - 网络:同节点 / 跨节点
- **产物**:**双写窗口的玩家感知阈值** = 知道多快算"够快"

### 实验 3(改):读副本 broadcast 带宽

- **目标**:测"一个 region 写一次,广播给 N 个 cold 副本"的带宽增长
- **变量**:N = 1 / 4 / 16 / 64
- **产物**:**读副本数量上限**——超过多少就改成"按需拉"而不是"广播推"

### 实验 4(原 5,补 D4):反作弊因果校验

D4 给了三个字段 + 两步校验,这个实验测实际开销。

- **目标**:测 CompositeEvent 加 OriginRegion/OriginTick/CausalityHash 后的带宽 + CPU 开销
- **变量**:
  - CausalityHash 长度:8B / 16B / 32B(碰撞概率)
  - 事件频率:每 tick 100 / 1000 / 10000
- **产物**:**因果校验的带宽成本 + 哈希长度的碰撞率**

---

## 已 advisor review 完成(2026-06-20)

review 结果:

1. D2 v3 的"单写权 + cold/hot + 双写过渡"模型 → ✅ 自洽
2. 实验 2 加"双写期间冲突类型分布"作为第 3 个核心测量 → ✅ 已加(可自动合并 / 需仲裁 / 不可合并 + 各自占比)
3. D5 / D-extra 因 D2 v3 变了需要调整 → ✅ D5 加 `replica_pods` 字段,D-extra 加 `write_authority_transfer` 请求类型
4. 实验 5(反作弊因果校验)优先级低 → ✅ 先埋指标,等实验 1-4 跑完再补

第二轮 re-review(2026-06-20,用户要求"再审核一遍")处理:

5. 删 4 段历史"两条路/问题"块(D1/D2/D5/D-extra)→ ✅ 删完,保留 advisor review 引文 + 已定方案
6. 实验 4(写权漂移玩家体验)合并到实验 2 的"1000 玩家场景"变量档位 → ✅ 合并完,实验从 5 变 4
7. D3 加 D2 v3 交叉引用(cold→hot 联动) → ✅ 加在 D3 末尾
8. D4 的 D2 交叉引用从"chunk 单 owner"改为"写权漂移 + cold/hot" → ✅

---

*advisor review 已完结(2026-06-20)。后续修订从 review 上下文里推,不再单独请 advisor 复审每一条。*
