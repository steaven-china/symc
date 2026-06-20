# WATCHDOG — symc project

This file is for the OMP advisor (read-only second model).
Primary agent's system prompt does not see this; the advisor does.

## Especially watch for

symc 是分布式 MC 同步引擎,基于 Paper fork。当前处于早期设计阶段,主 agent 倾向"自己推结论 + ponytail 模式少做"。5 个真正影响架构的开放决策**不应该主 agent 一人拍板**——advisor 必须 review:

1. **Weight 公式的 8 个因子权重 / 公式形态**(DECISIONS.md §D1)
2. **边界 chunk 双重缓冲:悲观锁 vs 乐观并发 vs 第三条路**(§D2)
3. **玩家跨区连接切换:proxy 黑盒 vs 客户端多连接**(§D3)
4. **反作弊基调:全 region 联署 vs 主 region + 抽查 vs 交给插件**(§D4)
5. **多 region 保存/加载协同:全局锁 vs 并行 + 二阶段提交**(§D5)

加一个 D-extra: CooperationRequest 协议细节(排队/误判回滚/限流/契约)。

## Review priorities

- **blocker** if: 主 agent 直接落了一个"已定决策"而没让我 review,或者把方案 B 的反对意见吞了
- **concern** if: 主 agent 给出"我自己推的"方案但跨过了 5 个待拍板的边界 / 公式里有明显数学问题 / 选了显然不是当前阶段该纠结的复杂方案
- **nit** if: README 排版 / ponytail 注释措辞 / DECISIONS.md 结构

## Working set to look at

- `README.md` — 全文(675 行,11 个主章节,设计讨论稿)
- `DECISIONS.md` — 5 个开放决策 + D-extra 协议细节,本文件配套
- `cmd/sim/main.go` + `pkg/{cell,layer,weight,affinity,scheduler,packet,sync}/` — Go sim 骨架
- `Paper/` — PaperMC/Paper `ver/26.1.2` 浅克隆(没 build,本机 JDK 21 < 25)
- `.gradle-env.ps1` — 隔离 Gradle 缓存到 D 盘

## Project context

- Owner language: 中文(Steaven Jiang)
- 风格偏好: 短促 / 证据优先 / 砍掉没必要的抽象
- 阶段: 设计早期,代码量极少,大部分工作是决策和文档
- 已克隆的 Paper 没改过(浅克隆,1 个 commit)
- C 盘有大小焦虑,任何默认往 C 盘用户目录塞东西的操作(Gradle 缓存、npm 全局、IDE 索引)要提醒重定向到 D 盘

## Constraints primary agent forgets

- 别直接给"5 个待拍板"任何之一出最终方案,advisor 还没说话
- 别碰 `Paper/` 内的文件(没改的 upstream,改之前要建 fork 分支)
- 别在 C 盘留 Gradle 缓存(已经设了 `GRADLE_USER_HOME` 重定向)
- ponytail 不等于"砍掉需要的东西"——D1/D2/D3/D4/D5 都是真需要决策的,不是可以砍的
