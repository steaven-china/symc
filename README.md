# symc — 分布式区域化 MC 的同步引擎思路

> 用大白话写的讨论稿。后面会整理成正式设计文档。
> 主题：把 MC 的世界切成 region，每个 region 独立跑，但玩家跨区域时感觉不到卡顿。

---

## 一、我们想解决什么问题

MC 服务端（比如 Paper）现在基本上是一个世界 = 一个进程。

玩家多了、红石多了、机器多了，单进程 CPU 和网络会爆。

我们想：
- 把地图分成多个 region（区域）
- 每个 region 跑在独立进程/机器上
- 玩家跨区域移动时不应该有加载画面或卡顿
- 远处没人的区域可以降低计算频率，省电省 CPU

类似网络里的 trunk：多根物理线绑成一根逻辑线，流量可以走不同路径，但对外看起来像一根线。

这里对应的是：多个 region server 拼成一个逻辑世界，玩家感觉还是一个连续的世界。

---

## 二、几个核心概念

### 1. Cell（最小处理单元）

一个 Cell 是三维的：

```
Cell = (chunk, tick, time_window)
```

- chunk：空间单位，比如一个 16x16 的区块
- tick：MC 逻辑 tick，20 tick = 1 秒
- time_window：真实时间窗口，比如 50ms、100ms、1s

为什么这三者不能拆？

- 拆 tick：同一个动作的两个 tick 被分开处理，顺序会乱，红石会坏
- 拆 chunk：一个动作跨两个 chunk，只处理一半，边界会坏
- 拆 time：预测和回滚会错

所以 `(chunk, tick, time_window)` 是一个**原子单元**，内部必须一起处理。

但同一个 Cell 产生的事件和数据，可以按后面的维度去打包、拆分、路由。

---

### 2. Layer（事件分层）

一个 Cell 里会发生各种各样的事件。我们按"变化多快、对玩家多重要"分成四层：

#### 静态层（Static）

- 基本不变的东西
- 比如石头地形、光照贴图（稳定后）、生物群系
- 可以很长时间 tick 一次，甚至不 tick
- 同步允许慢，允许延迟

#### 半静态层（Semi-static）

- 平时不动，但可能被某个事件唤醒
- 比如休眠的红石机器、没物品的漏斗链、睡觉的动物
- 稳定时按低频 tick，被触发后提升到动态层
- 这一层做好了能省很多性能

#### 动态层（Dynamic）

- 必须 20 tick 跑满的东西
- 玩家移动、战斗、掉落物、TNT、活跃红石、怪物 AI
- 低延迟、强一致
- 必须优先保证

#### 瞬态层（Ephemeral）

- 丢了也没关系的东西
- 粒子、声音、远处实体的粗略位置
- 客户端可以插值补出来
- 可以降采样、走不可靠通道

---

### 3. Weight（竞争权重）

每个 Cell 在不同 Layer 下，会有一个权重。

权重高 = 这次 tick 必须优先处理、优先发包。

影响权重的因素：

- 里 deadline 多近（ urgency ）
- 影响多少玩家/实体（ impact ）
- 当前 region CPU 负载（ load ）
- 同步这个数据要花多少带宽（ network cost ）
- 一致性要求多高（ consistency ）
- 和邻居 cell 打包能不能省 overhead（ batch gain ）
- 拆开会不会导致额外同步（ split penalty ）

权重不是固定的，每 tick 都会重新算。

---

### 4. Region（区域服务器）

一个 Region Server 负责一片 chunk。

- 只加载自己负责的区域
- 定时向中央 scheduler 汇报负载
- 和其他 region 通过 sync trunk 同步边界状态

一个 region 可以包含很多 chunk，也可以只有一个 chunk，取决于负载。

热点区域（很多玩家、很多红石）可以拆成多个 region。
冷点区域（野外、空置农场）可以合并成一个大 region。

---

## 三、数据怎么流动

```
玩家操作 ──→ Region Server A
                │
                ▼
        生成 Cell（chunk, tick, time）
                │
                ▼
        判断 Layer（static/semi/dynamic/ephemeral）
                │
                ▼
        计算 Weight
                │
                ▼
        按 Affinity 聚类（相邻 chunk、同 region、同事件源）
                │
                ▼
        按 Network budget 切成 packet
                │
                ▼
        发送给需要的人/其他 region/客户端
```

---

## 四、和"网络 trunk"的类比

| 网络 trunk | symc |
|-----------|------|
| 多根物理线 | 多个 region server |
| 一根逻辑线 | 一个连续的 MC 世界 |
| VLAN tag 区分不同逻辑网络 | Layer tag 区分事件优先级 |
| 链路聚合 | 把同方向的 cell 打包发送 |
| QoS | 高权重 cell 优先发 |
| 链路故障切换 | region 崩溃时 cell 重路由 |
| 带宽动态分配 | 根据 region 负载调整 tick budget |

---

## 五、关键难点（目前想到的）

### 1. 边界一致性

两个 region 交界处，玩家左边在 A，右边在 B：

- 玩家跨区移动怎么无缝切换？
- 红石信号跨区怎么不延迟/不乱序？
- 水流、漏斗、怪物 AI 跨区怎么处理？
- 两个 region 的 tick 时间必须对齐，否则边界会抖

可能的思路：
- 边界 chunk 做双重缓冲
- 跨区事件走 fast sync 通道
- 红石这种强一致的，强制放在同一个 region 内处理

### 2. 玩家跨区迁移

玩家从 region A 走到 region B：

- 玩家实体状态要完整迁移
- 背包、效果、坐骑、速度向量都不能丢
- 客户端不能看到加载画面
- 原 region 要告诉周围玩家"这个人消失了"，新 region 要告诉周围玩家"这个人出现了"

可能的思路：
- 用 proxy 层做连接切换
- 或者同时维护多个 region 连接，切换主连接

### 3. 红石/漏斗/水流

这些天然会跨很多 chunk：

- 一个 1-tick 红石脉冲跨区，延迟必须确定
- 漏斗链可以穿几十个 chunk
- 水流从 A region 流到 B region

如果 A 和 B 的 tick 不同步，红石全坏。

可能的思路：
- 对跨区红石做限制
- 或者把整个强交互红石网络放在同一个 region
- 动态检测红石网络，必要时合并 region

### 4. 事件分层误判

以为某个东西是静态的，结果它突然被触发：

- 比如一个长期不用的红石机器被玩家激活
- 升层需要时间，这期间可能丢 tick

可能的思路：
- 快速升层机制
- 保留最近几 tick 的事件缓冲
- 升层后补发未处理的事件

### 5. 世界生成一致性

不同 region 生成相邻地形时：

- 噪声函数在边界必须一致
- 结构（村庄、要塞）可能跨 region

这个 Paper 的生成器本身是确定性的，但要保证分片后仍然 deterministic。

### 6. 大量实体跨区

掉落物、箭、TNT 数量巨大：

- 突然大量跨区会变成网络风暴
- 需要限流、合并、丢弃低优先级实体

---

## 六、最小可验证原型（MVP）

### 阶段 1：先写 simulator

不碰 Paper，先写一个 standalone 的 cell scheduler 模拟器：

输入：
- region 数量
- chunk 数量
- 每 tick 动态/静态事件比例
- 网络带宽限制
- 玩家分布热点

输出：
- 每 tick 发了多少 packet
- 带宽用了多少
- 各 layer 延迟分布
- 边界不一致事件次数

目标：验证"tick-chunk-time 绑定 + layer 分流 + weight 竞争"这个想法能不能省带宽和省 CPU。

### 阶段 2：接 Paper

在 Paper fork 里：
- 只让实例负责 x<0 或 x>=0 的 chunk
- 每个 tick 把事件输出成 cell 格式
- 用 simulator 的逻辑决定怎么发包
- 先在 proxy 层做玩家跨区迁移

### 阶段 3：动态 region

根据负载自动拆分/合并 region。

---

## 七、代码目录设想

```
D:/engine/symc/
├── README.md            # 这个文件
├── DESIGN.md            # 后续整理成正式设计文档
├── cmd/
│   └── sim/
│       └── main.go      # 调度模拟器
├── pkg/
│   ├── cell/            # Cell 定义
│   ├── layer/           # 四层事件分类
│   ├── weight/          # 权重计算
│   ├── affinity/        # 聚类逻辑
│   ├── scheduler/       # tick budget 分配
│   ├── packet/          # 分包逻辑
│   └── sync/            # region 间同步协议
└── proto/               # 事件序列化格式
```

---

## 八、和现有项目的对比

| 项目 | 在做什么 | 我们的区别 |
|-----|---------|-----------|
| Paper | 单进程优化 | 我们要跨进程 |
| Folia | 单进程内按 region 并行 tick | 我们要跨进程 + 动态调度 |
| Minestom | 从头写的分布式友好框架 | 我们基于 Paper fork，保留原版行为 |
| Velocity/BungeeCord | 跨服传送 | 我们要无感知迁移 |

空白地带：基于 Paper、跨进程、动态 region、tick-chunk-time 调度的方案。

---

## 九、还没想清楚的问题

1. Cell 的 time_window 到底取多长？和 tick 的关系是什么？
2. Weight 公式里的各参数权重怎么调？有没有可观测指标？
3. 边界 chunk 的双重缓冲具体怎么实现？锁还是乐观并发？
4. 玩家跨区时，客户端是单连接切换还是多连接？
5. 静态层和半静态层切换的触发条件是什么？
6. region 拆分/合并的触发条件是什么？
7. 是否需要改客户端？如果不改，proxy 层要黑盒做到什么程度？
8. 这个架构下怎么保证反作弊？
9. 保存/加载世界时，多个 region 怎么协同？
10. 网络分区时怎么办？是暂停还是允许临时不一致？

---

## 十、一句话总结

> 把 MC 世界切成多个 region 跑，但玩家感觉不到。关键不是切得多细，而是 tick-chunk-time 三者绑成原子单元，然后用 layer/weight/affinity/budget 去打包、路由、降级。

---

## 十一、边界问题的新理解：复合事件 + 权重化计算

边界不是地理上的"这条线左边归 A，右边归 B"。

边界其实是**事件影响范围的交集**。一个事件同时涉及多个 region 时，它就是一个边界事件。

我们不把边界当特殊情况处理，而是把所有跨区事件抽象成统一的 **CompositeEvent（复合事件）**，然后用权重化计算决定怎么处理。

### 11.1 复合事件是什么

```
CompositeEvent = {
    ID: 事件唯一ID,
    Type: 事件类型（玩家跨区、红石跨区、水流跨区、实体迁移…）,
    Cells: [涉及的 (chunk, tick, time_window)],
    Regions: [涉及的 region],
    Weight: 整体权重,
    Consistency: 一致性要求（strict / eventual / best_effort）,
    Partials: [各 region 贡献的部分计算结果]
}
```

关键思想：
- 不是"哪个 region 说了算"
- 而是"这个复合事件权重多高、一致性多强、该由谁参与计算"

### 11.2 复合计算：不是每个 region 都算一遍

传统想法：事件跨 A 和 B，那 A 算一遍，B 也算一遍，然后同步。

这样浪费，而且容易冲突。

复合计算的意思是：
- 一个复合事件由多个 region **共同订阅**
- 但只让必要的 region 做必要的计算
- 其他 region 接收结果，或者只做轻量校验

比如玩家跨区移动：
- region A（玩家脚所在侧）：算完整物理 + 碰撞
- region B（另一侧）：只同步位置和速度向量
- region C（远处观察者）：可能只收到一个粗略位置

这就是"按贡献度分配计算"。

### 11.3 计算贡献度怎么定

每个 region 对一个 CompositeEvent 有一个 **ComputeContribution（计算贡献度）**：

```
ComputeContribution = f(
    是否拥有该事件的主 chunk,
    当前 region 负载,
    是否需要精确结果,
    历史计算能力,
    网络延迟
)
```

规则示例：

| 场景 | 主计算 region | 其他 region |
|-----|-------------|-----------|
| 玩家跨区移动 | 脚所在侧 region | 同步位置/速度 |
| 红石跨区 | 信号源所在 region | 接收信号状态 |
| 实体被射中 | 箭的 region 算轨迹，玩家 region 算命中 | 同步结果 |
| 远处爆炸 | 爆炸中心 region 全算 | 其他只接收粒子/声音 |

贡献度不是固定的，每 tick 根据负载重新分配。

### 11.4 权重化计算

每个 CompositeEvent 有一个 Weight，决定它占用多少资源。

#### 基础权重因子

```
Urgency      : 离 deadline 多近
Impact       : 影响多少玩家/实体
Consistency  : 一致性要求多高
Load         : 当前 region CPU 负载
NetworkCost  : 同步这个数据要花多少带宽
BatchGain    : 和邻居打包能省多少 overhead
SplitPenalty : 拆开会导致多少额外同步
RegionCount  : 涉及几个 region
```

#### 权重公式（第一版）

```
Weight = (Urgency * Impact * Consistency * RegionFactor) 
         / (Load * NetworkCost)
         + BatchGain
         - SplitPenalty
```

其中：
```
RegionFactor = 1.0 + 0.3 * (RegionCount - 1)
```

涉及 region 越多，权重越高，因为越容易出错、越需要资源。

#### 权重怎么用

| Weight 范围 | 处理方式 |
|------------|---------|
| > 0.8 | fast sync，所有相关 region 必须同步，tick 对齐 |
| 0.5 ~ 0.8 | 主 region 权威，其他 region 按需接收 |
| 0.2 ~ 0.5 | 允许延迟，允许降采样 |
| < 0.2 | 可以合并到下一个 tick，甚至丢弃（仅限 ephemeral） |

### 11.5 冲突消解

多个 region 对同一个复合事件给出不同 partial result 时：

1. **主 region 权威制**：指定一个主 region，它的结果为准。简单但主 region 选择要稳定。
2. **投票制**：多个 region 投票或加权平均。适合可插值场景。
3. **向量时钟**：每个 partial result 带版本向量，合并时判断因果。
4. **按权重分配精度**：主 region 全精度，次 region 按需精度，观察者 region 降采样。

实际实现可能是混合：
- 玩家移动：主 region 权威
- 远处实体位置：投票/加权平均
- 红石信号：向量时钟严格对齐

### 11.6 三个具体例子

#### 例子 1：玩家跨区移动

```
CompositeEvent: player_cross_region
Regions: [A, B]
Cells: [chunk_A, chunk_B, tick_100, 50ms]
Weight: 1.0 * 1.0 * 1.0 * 1.3 / (0.5 * 0.5) = 5.2
Consistency: strict
```

处理：
- A 是主 region，算完整物理
- B 同步位置和速度
- 如果玩家在边界来回蹭，提升 BatchGain，减少反复切换
- 网络抖动时，B 短暂 extrapolate，A 权威校正

#### 例子 2：红石跨区

```
CompositeEvent: redstone_cross_region
Regions: [A, B]
Cells: [chunk_A, chunk_B, tick_100, 50ms]
Weight: 1.0 * 0.8 * 1.0 * 1.3 / (0.5 * 0.2) = 10.4
Consistency: strict
```

处理：
- 权重比玩家移动还高，因为 consistency 高、network cost 低
- 强制走 fast sync
- A 和 B 的 tick 必须对齐
- 如果频繁发生，系统提议合并 A 和 B 的相关 chunk

#### 例子 3：水流跨区

```
CompositeEvent: fluid_cross_region
Regions: [A, B, C]
Cells: [chunk_A, chunk_B, chunk_C, tick_100~101, 100ms]
Weight: 0.6 * 0.5 * 0.9 * 1.6 / (0.7 * 0.7) = 0.88
Consistency: eventual
```

处理：
- 权重中等
- 玩家不在附近时，降到 semi-static
- 玩家靠近时，提升到 dynamic
- 网络 budget 不够时，允许 100ms 延迟

### 11.7 边界问题最终收敛

用复合事件 + 权重化计算后，边界问题变成：

1. 没有"边界特殊情况"
2. 不是"哪个 region 说了算"
3. 边界是事件影响范围的交集
4. 系统按权重动态决定：谁计算、谁同步、谁降级、是否合并 region

带来的好处：

| 传统做法 | 我们的做法 |
|---------|-----------|
| 边界 chunk 必须锁 | 复合事件按权重决定锁范围 |
| 跨区 = 高成本特殊路径 | 跨区 = 普通事件，只是权重高 |
| region 拆分需要人工设计 | 系统根据复合事件频率自动决定合并不合并 |
| 所有边界一样处理 | 玩家边界 ≠ 红石边界 ≠ 水流边界 |
| 每个 region 都算一遍 | 按贡献度分配计算 |

---

*最后修改：2026-06-20*
*状态：大白话讨论稿，未整理*

---

## 十二、复杂情况的协作计算：O(n - k) 与合作申请

### 12.1 核心直觉

一个复合事件涉及 n 个 region 时，不是每个 region 都要算一遍。

其中有很多运算是"多余的、确定的、无竞争的"：
- 红石信号转发规则是确定的
- 箭在本区域内的轨迹只和本地状态有关
- 水流规则是固定的
- 静态地形查询谁查都一样

这些部分只需要一个 region 算，其他 region 接收结果。

真正需要多个 region 协商竞争的，只有 k 个部分。

所以协作计算的复杂度可以从 O(n) 降到 **O(n - k)**。

### 12.2 谁向谁申请

规则：

> **活跃节点（Dynamic region）向静态可变节点（Static-Mutable region）发起合作申请。**

前提是：
1. 集群已经正确配置
2. 同步协议支持这个步骤
3. 静态可变节点可达

| 节点类型 | 特征 | 在协作中的角色 |
|---------|------|--------------|
| Dynamic（活跃节点） | tick 满速、玩家密集、事件高频 | 发起方，知道当前需要什么 |
| Static（静态节点） | 几乎不 tick，只保存状态 | 被动提供历史/基线数据 |
| Static-Mutable（静态可变节点） | 平时 static，被触发时可升级 | 被申请方，负责无竞争运算 |

### 12.3 申请流程

```
Dynamic Region A 遇到跨区复合事件
        ↓
发现需要 Static-Mutable Region B 的数据
        ↓
发送 CooperationRequest
        ↓
Region B 收到申请
        ↓
判断：这部分运算是否无竞争？
        ↓
是 → 直接计算 → 返回 partial result
否 → 唤醒升级到 dynamic → 参与竞争计算
        ↓
Region A 汇总所有 partial results
        ↓
冲突消解 → 输出最终结果
```

### 12.4 CooperationRequest 里有什么

```
CooperationRequest = {
    EventID:        复合事件ID,
    FromRegion:     发起 region,
    ToRegion:       目标 static-mutable region,
    EventType:      事件类型,
    NeededCells:    [需要的 (chunk, tick, time_window)],
    StaticParts:    [请求对方算的无竞争部分],
    DynamicParts:   [如果对方需要升级才参与的部分],
    Deadline:       最晚回复时间,
    MergeStrategy:  结果汇总方式
}
```

Static-Mutable region 回复：

```
CooperationResponse = {
    EventID:        对应事件ID,
    RegionID:       回复 region,
    Accepted:       是否接受,
    Result:         partial result（如果无竞争部分直接算完）,
    WillUpgrade:    是否需要升级到 dynamic,
    NeedsResults:   [还需要发起方提供的其他结果],
    EstimatedCost:  预计计算/网络开销
}
```

### 12.5 例子：红石网络跨 5 个 region

信号从 A → B → C → D → E

传统：A、B、C、D、E 各算一遍，O(5)

实际：
- A 是 dynamic region，发现信号要跨区
- A 向 B、C、D、E 发 CooperationRequest
- B、C、D、E 是 static-mutable region
- 信号转发规则是确定的，属于无竞争运算
- 只有 A 需要算信号源状态
- B、C、D、E 接收信号并转发

n = 5, k = 4
协作计算复杂度 ≈ O(1)

### 12.6 例子：玩家跨区射箭

A region 的玩家 P 向 B region 的玩家 Q 射箭。

- A 是 dynamic region，发起 CooperationRequest 到 B
- A 自己算箭的轨迹（无竞争）
- B 收到申请后，判断命中判定需要竞争
- B 升级到 dynamic，参与命中计算
- A 汇总结果

n = 2, k = 1

真正需要协作的只有命中判定，O(1)


### 12.7 无竞争运算的类型

| 类型 | 例子 |
|-----|------|
| 纯本地物理 | 箭在本 region 内的轨迹 |
| 确定性规则 | 红石信号传播、水流流动规则 |
| 只读查询 | 远处玩家位置查询 |
| 可预测插值 | 客户端预测的位置 |
| 静态/半静态数据 | 地形、光照、休眠机器的状态 |

### 12.8 关键约束

1. 只有 **活跃节点** 能发起申请
2. 只有 **静态可变节点** 能接收申请并被唤醒
3. 纯静态节点只返回数据，不参与计算
4. 申请是**按需的**，不是每 tick 都发
5. 如果 static-mutable 节点拒绝或超时，发起方要降级处理

### 12.9 还需要想清楚的问题

1. 如果多个 dynamic region 同时向同一个 static-mutable region 申请，怎么排队？
2. static-mutable region 升级成 dynamic 后，什么时候降级回 static？
3. 申请超时了怎么办？是降级还是重试？
4. 无竞争运算的判断是谁做的？发起方还是接收方？
5. 如果接收方误判为无竞争，实际有竞争，怎么回滚？
6. 合作申请的频率要不要限流？
7. 是否需要预建立合作契约，而不是每 tick 临时申请？

*最后修改：2026-06-20*
*状态：大白话讨论稿，未整理*
