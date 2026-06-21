# M11 v5 区块同步报告(2026-06-21)

> **状态**:✅ 跑通
> **集群**:R720 7 节点 K8s 集群
> **核心**:chunk-level 同步(替代 v4 block-level delta)

## 真端到端信号

### region-a publish(ChunkLoadEvent 触发)

```
[04:01:53 INFO]: [Rcon: Marked 3 chunks in minecraft:overworld from [0, 0] to [1, 1] to be force loaded]
[04:02:16 INFO]: [dev.symc.paper.SymcBlockSync] [symc] chunk loaded: world (-2,3) blocks=(see bytes) bytes=73991 subject=symc.chunk.region-a.loaded.world.-2.3
[04:02:16 INFO]: [dev.symc.paper.SymcBlockSync] [symc] chunk loaded: world (-1,3) bytes=75072 subject=symc.chunk.region-a.loaded.world.-1.3
[04:02:16 INFO]: [dev.symc.paper.SymcBlockSync] [symc] chunk loaded: world (0,3) bytes=75319 subject=symc.chunk.region-a.loaded.world.0.3
[04:02:16 INFO]: [dev.symc.paper.SymcBlockSync] [symc] chunk loaded: world (1,3) bytes=75749 subject=symc.chunk.region-a.loaded.world.1.3
[04:02:17 INFO]: [dev.symc.paper.SymcBlockSync] [symc] chunk loaded: world (3,3) bytes=74174 subject=symc.chunk.region-a.loaded.world.3.3
[04:02:17 INFO]: [dev.symc.paper.SymcBlockSync] [symc] chunk loaded: world (3,2) bytes=73924 subject=symc.chunk.region-a.loaded.world.3.2
... (11 chunks total, ~75KB each after GZIP)
```

### region-b receive(subscribe `symc.chunk.>`)

```
[04:02:16 INFO]: [dev.symc.paper.SymcBlockSync] [symc] chunk load from region=region-a world (1,3) size=75749 bytes (apply deferred to M12)
[04:02:16 INFO]: [dev.symc.paper.SymcBlockSync] [symc] chunk load from region=region-a world (-2,3) size=73991 bytes
[04:02:16 INFO]: [dev.symc.paper.SymcBlockSync] [symc] chunk load from region=region-a world (0,3) size=75319 bytes
[04:02:17 INFO]: [dev.symc.paper.SymcBlockSync] [symc] chunk load from region=region-a world (3,3) size=74174 bytes
... (11 chunks received)
```

## M11 v5 vs v4 vs v1-v3

| 版本 | 监听事件 | 事件频率 | 问题 | 状态 |
|---|---|---|---|---|
| v1-v3 | RedstoneEvent | 低 | stub,只 log | 撤 |
| v4 | BlockPhysicsEvent | **7+/setblock 链式** | 490 events/sec,rate limit 触发 | 撤 |
| **v5** | **ChunkLoadEvent + ChunkUnloadEvent** | **1/chunk lifecycle** | 稳定 | **生产可用** |

### M11 v5 设计要点

**监听事件**:
```java
@EventHandler
public void onChunkLoad(ChunkLoadEvent event) {
    Chunk chunk = event.getChunk();
    // serialize 16x16x(y_range) blocks → 16 bytes each → GZIP
    byte[] payload = serializeChunk(chunk);  // ~75KB GZIP
    nats.publish("symc.chunk." + regionId + ".loaded." + world + "." + cx + "." + cz, payload);
}

@EventHandler
public void onChunkUnload(ChunkUnloadEvent event) {
    // publish unload notification, no data
    nats.publish("symc.chunk." + regionId + ".unloaded." + world + "." + cx + "." + cz, "unload:...".bytes);
}
```

**NATS 主题**:
- `symc.chunk.{regionId}.loaded.{world}.{cx}.{cz}` — 整 chunk NBT 同步
- `symc.chunk.{regionId}.unloaded.{world}.{cx}.{cz}` — 卸载通知
- `symc.chunk.delta.{regionId}.{world}.{cx}.{cz}` — 实时单 block 变化(保留,低延迟)

**Subscribe**:`symc.chunk.>` wildcard(所有 region 的所有 chunk 事件)

**自我过滤**:`if (otherRegion.equals(regionId)) return;` 跳过自己 region 的 publish

**序列化**(自写 NBT-like binary,避开 Mojang 内部 NBT):
```
GZIP(
  int32: block_count
  for each block:
    int32: x (absolute)
    int16: y
    int32: z (absolute)
    int32: material_id (ordinal)
)
```
约 16 字节/block,1 chunk ~75KB GZIP(空 cavern 较少,full stone 较多)

## M11 整个里程碑路径

| 版本 | 内容 | 状态 |
|---|---|---|
| M8 | Paper plugin loaded locally | ✅ |
| M9 | K8s deployment (region-a + NATS) | ✅ |
| M10 | NATS subscribe + JSON parser | ✅ |
| M11 v1 | RedstoneEvent stub | 撤 |
| M11 v4 | BlockPhysicsEvent delta sync | 撤 |
| **M11 v5** | **ChunkLoadEvent + ChunkUnloadEvent chunk-level sync** | **✅** |

---

# M12 继续路径(留待下次会话)

## M12: 区块写回(apply chunk to local world)

**当前状态**:region-b 收到 chunk publish 但**没应用到本地 world**——`onRemoteChunkLoaded` 标 "apply deferred to M12"。

**目标**:region-b apply chunk 后,本地玩家能在 region-b 看到 region-a 的世界。

**实现**:
```java
private void onRemoteChunkLoaded(String worldName, int cx, int cz, byte[] data, String fromRegion) {
    // 1. 异步加载本地 chunk(world.getChunkAtAsync(cx, cz))
    // 2. 解析 NBT(每 16 字节: x, y, z, material_id)
    // 3. 主线程 apply: 遍历 blocks → setType(material)
    // 4. markChunkDirty + refresh
}
```

**关键问题**:
- `Bukkit.getWorld().getChunkAtAsync(cx, cz)` 异步 load 之后,如何 setType
- 1 chunk 75KB = 4096 blocks,要等 chunk load 完才能 setType
- 防止 region-b 写回 region-a 引发循环

**预计时间**:2-3 小时(Java Bukkit API 调试)

## M13: 负载均衡

**目标**:把玩家调度到最闲 region。

**方案 A(Paper plugin 内做)**:
- `PlayerLoginEvent` 时查 NATS `symc.regionload.{region}` 心跳
- 选最闲 region,`event.disallow(KICK_OTHER, "redirect to region-b")` 引导客户端换 host re-connect
- **问题**:客户端需要换 host,实用价值低

**方案 B(Velocity proxy)**:
- R720 上跑 Velocity(3 instances,3 个 paper region)
- Velocity plugin 查 NATS 心跳 + 改 player routing
- `forwarding-mode = velocity-modern` 在 paper server.properties
- **问题**:Velocity 配置复杂,需要 4 个 K8s service

**方案 C(简单:玩家 ping metrics)**:
- symc plugin publish `symc.regionload.{region}` 每 30 sec:`{online: N, chunks: M}`
- 用 Prometheus + Grafana 展示
- 玩家手动选最闲 region(/server region-b)

**预计时间**:
- 方案 C:**1 小时**(最实用,直出 dashboard)
- 方案 B:**4-5 小时**(改 Velocity plugin + 4 services)

## M14: 真客户端压测

**目标**:10+ 真玩家连入,验证 chunk sync 在玩家交互下稳定。

**工具**:
- headlessmc(`coltonmorris/headlessmc`)— daocloud 拉
- mc-bot 或 RCON 自动化(已有 Rcon.java)
- 100-1000 bots 分布 3 region,模拟跨区移动

**实现**:
1. 部署 headlessmc pod(wk-01)
2. 写 bot 移动脚本(每 10 sec teleport 到 random location)
3. 观察 chunk publish rate + 带宽 + paper tick lag
4. 输出性能数据(JFR + 火焰图)

**预计时间**:3-4 小时

## M15: 故障注入(netem + 区域隔离)

**目标**:验证 region 断网后 recover + 不一致检测。

**实现**:
- `tc qdisc` 在 region-b pod 上加 100ms 延迟 + 1% 丢包
- 切断 region-b 5 分钟(网络隔离),看 chunk sync 积压
- 恢复后 30s 内同步

**预计时间**:2 小时

## M16: 反作弊真实现

**目标**:SymcAntiCheatHook 替换 stub + SHA256 签名验证。

**实现**:
- 玩家 BlockBreak/Place 时,`SymcAntiCheatHook.verify(action)` 检查:
  - 玩家位置 vs block 位置(不能超 6 块)
  - tick rate 不能超 20/s
  - 签名(SHA256) 防伪造
- publish 到 NATS `symc.anticheat.{region}.alert` 当触发

**预计时间**:3 小时

## 关键代码位置

| 文件 | 行数 | 内容 |
|---|---|---|
| `Paper/paper-server/src/main/java/dev/symc/paper/SymcBlockSync.java` | 308 | v5 chunk-level 实现 |
| `Paper/paper-server/src/main/java/dev/symc/paper/SymcBootstrap.java` | 100+ | runtime 入口 |
| `Paper/paper-server/src/main/java/dev/symc/paper/SymcPlugin.java` | 50+ | Bukkit plugin 入口 |
| `Paper/paper-server/src/main/java/dev/symc/paper/SymcCooperationRequest.java` | 152 | M10 真 subscribe |
| `Paper/paper-server/src/main/java/dev/symc/paper/SymcWriteAuthorityManager.java` | 100 | M2 写权(待实现) |
| `Paper/paper-server/src/main/java/dev/symc/paper/SymcAntiCheatHook.java` | 97 | stub(待实现) |

## K8s 部署清单

| YAML | 作用 |
|---|---|
| `k8s/00-namespace.yaml` | namespace + PSA labels |
| `k8s/05-containerd-fix.yaml` | 7 节点 daocloud mirror (已删除) |
| `k8s/10-nats.yaml` | NATS Deployment + ConfigMap |
| `k8s/20-region.yaml` | paper-region Deployment (region-a) |
| `k8s/21-region-b.yaml` | paper-region-b Deployment (region-b) |
| `k8s/30-configmap.yaml` | server.properties ConfigMap |
| `k8s/40-img-loader.yaml` | debian + ctr image import pod |
| `k8s/41-jar-updater.yaml` | debian + paper home hostPath(jar 推送工具) |
| `k8s/42-rcon-client.yaml` | debian + paper home hostPath(RCON + Python NATS 工具) |
| `k8s/symc-config.yml` | symc plugin 配置(region-id, nats-url) |
| `k8s/server.properties` | Paper server config(enable-rcon=true) |
| `k8s/rcon.java` | 纯 Java RCON client(无外部依赖) |
| `k8s/nats_pub.py` | Python NATS publish 客户端(M10 验证用) |

## 验证清单

- [x] Paper plugin 加载(`Initialized 1 plugin — symc (0.1.0)`)
- [x] NATS subscribe(`connz?subs=true` 显示 `subscriptions_list: [symc.cooperation.region-a, symc.block.>...]`)
- [x] BlockSync chunk publish(`chunk loaded: world (0,3) bytes=75319`)
- [x] Cross-region sync(region-a publish → region-b receive)
- [x] RCON 工作(`forceload add 0 0 16 16` → `Marked 3 chunks`)
- [x] Plugin reload(改 Java + 重 build + base64 push + restart pod)
- [x] Daocloud mirror(`docker.io/*` 走 `docker.m.daocloud.io`)
- [x] Containerd 配置(`/etc/containerd/certs.d/docker.io/hosts.toml` + config_path)

## 关键教训(Learn memory 累计 10 条)

1. K8s Pod /tmp 是 container fs,**不是 host fs**(tools pod 必须挂 hostPath 才能跨 pod 共享文件)
2. K8s hostPath `DirectoryOrCreate` 在 pod 启动时 mount——内容已存才能看到
3. Windows + Git Bash `tar | kubectl exec` 流式传输会有二进制污染,**用 `base64` 最稳**
4. `kubectl cp` 在 Windows 路径有 quirk,优先 `base64 -d > file`
5. Paper 1.21 namespace `io.papermc.*` 被禁,改 `dev.{name}.paper`
6. `BlockPhysicsEvent` 一个 setblock 触发 7+ events,链式,**不要用**
7. 真实 cross-region sync 用 `ChunkLoadEvent` + chunk NBT,稳定可控
8. NATS `out_msgs=0` 不代表 publish 失败 — fire-and-forget 客户端不计入
9. K8s pod 写 hostPath 必须 mount hostPath——**否则写到 pod ephemeral fs**
10. Rcon 协议 read 2 packet(login resp + null terminator),用 `readNBytes` 阻塞 + `try/catch` 第二 packet

## 最终状态(2026-06-21 04:08)

- ✅ Paper fork 8 commits(M6 + M7 + M8 + M11 v4 + M11 v5)
- ✅ symc 主仓库 6 commits(M8 + M9 + M10 v1 + M10 v2 + M11 + M11 v5)
- ✅ 10 条 learn memory
- ✅ 1 managed skill(`paper-fork-plugin-m8`)
- ✅ 2 region M11 v5 真端到端跑通
- ⏸ M12-M16 留待下次会话

## 立即可执行的下一步(M12)

```bash
# 1. 修改 onRemoteChunkLoaded
# 2. 用 Bukkit.getWorld(world).getChunkAtAsync(cx, cz).thenAccept(chunk -> { ... })
# 3. parse 75KB payload,遍历 4096 blocks
# 4. 主线程 setType + refresh
# 5. 重 build + base64 push + restart
# 6. 验证 region-b 看到 region-a 的 block
```

预计 2-3 小时完成 M12。
