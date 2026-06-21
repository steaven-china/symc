# M10+ 路线图(2026-06-21)

> **M9 已完成**:Paper + symc + NATS K8s 跑通,但 3 个 listener 是 stub

## M9 当前状态

```
paper-region-9486dd9c9-j29v8  1/1 Running  (k8s-wk-01, region=paper-region)
nats-8fc7854cb-nc9lq          1/1 Running  (k8s-wk-02, JetStream ready)
```

symc plugin loaded + bootstrap done,**但**:
- ❌ SymcCooperationRequest:只打 "started" 日志,**没 subscribe NATS subject**
- ❌ SymcWriteAuthorityManager:只打 "started" 日志,**没真写权仲裁**
- ❌ SymcAntiCheatHook:只打 "started" 日志,**没真校验**
- ❌ NATS 消息流:**0 msgs/s**(无 client subscribe + 无 publish)

**结论**:M9 是"框架跑通",**不是"系统工作"**。

## M10:真 listener 实现

### 优先级 1:SymcCooperationRequest

**目标**:Bukkit 红石事件 → NATS publish → 其他 region 收到

**实现**:
```java
// SymcCooperationRequest.java
@EventHandler
public void onBlockRedstoneEvent(BlockRedstoneEvent event) {
    // 1. 收集 event 数据
    SymcBlockEvent ev = new SymcBlockEvent(
        regionId,
        event.getBlock().getWorld().getName(),
        event.getBlock().getX(), event.getBlock().getY(), event.getBlock().getZ(),
        event.getNewCurrent()
    );
    // 2. publish 到 NATS subject
    String subject = "symc.cooperation." + regionId;
    natsConnection.publish(subject, ev.toJson().getBytes());
}
```

### 优先级 2:SymcWriteAuthorityManager

**目标**:跨 region 写权漂移检测(双写冲突检测)

**实现**:
```java
// SymcWriteAuthorityManager.java
public CompletableFuture<WriteGrant> requestWriteGrant(SymcBlockPos pos, long tick) {
    // 1. 查 local write authority
    SymcRegionResolver.authorityOf(pos) -> local or remote
    // 2. 若 remote,publish 到 remote region subject
    String subject = "symc.writegrant." + remoteRegion;
    WriteGrantRequest req = new WriteGrantRequest(pos, tick, regionId);
    natsConnection.publish(subject, req.toJson().getBytes());
    // 3. 监听 reply subject 等 ack
    return CompletableFuture.completedFuture(grant);
}
```

### 优先级 3:SymcAntiCheatHook

**目标**:8B hash 不够,换 SHA256 + 位置签名

```java
// SymcAntiCheatHook.java
public boolean verify(SymcBlockAction action) {
    String payload = action.position() + action.tick() + action.playerId();
    String hash = sha256(payload + sharedSecret);
    return hash.equals(action.signature());
}
```

## M11:多 region scale out

```bash
# Apply region-b / region-c
kubectl apply -f k8s/21-region-b.yaml
kubectl apply -f k8s/22-region-c.yaml

# 验证:每 region 都连 nats,subject "symc.cooperation.{region}"
# 跨 region 红石事件 publish/subscribe
```
**Java 源在 Paper fork git history** —— 恢复不需要反编译:
```bash
cd /d/engine/symc/Paper
git checkout 29585ac -- paper-server/src/main/java/dev/symc/paper/
# 恢复 7 个 .java 文件:
# - SymcAntiCheatHook.java
# - SymcBootstrap.java
# - SymcCooperationRequest.java
# - SymcPlugin.java
# - SymcWriteAuthorityManager.java
# - package-info.java
# (M8 commit 29585ac 之前有 SymcCooperationRequest.java / SymcWriteAuthorityManager.java / SymcAntiCheatHook.java 是 stub 版本)
```

**重写流程**:
1. 改 SymcCooperationRequest.java:Subscribe NATS subject + hook BlockRedstoneEvent
2. 改 SymcWriteAuthorityManager.java:requestWriteGrant() + NATS pub/sub for cross-region
3. 改 SymcAntiCheatHook.java:SHA256 签名 verify
4. 用 Paper 编译 classpath 重编译(./gradlew.bat :paper-server:compileJava + 自定义 task 重打 plugin jar)
5. 推到 K8s hostPath(`kubectl exec img-loader -- sh -c 'cp new-jar /tmp/paper/plugins/symc-plugin-0.1.0.jar'` + delete paper-region pod)
6. 验证 NATS 消息流(`nats sub 'symc.cooperation.>'` 收到 publish)

**预计 2-3 小时**(改 3 文件 + 重编 + 重部 + 验证)
**Java 源码已删** —— 重新生成需要:
- 反编译 `symc-plugin-0.1.0.jar` (用 cfr / procyon)
- 重写 stub → real listener
- 用 `javac` 重编译(用 Paper 编译 classpath,包括 Bukkit API + jnats 2.17.0)
- jar 重建 → push 到 K8s hostPath

**预计 2-3 小时**(反编译 + 重写 + 重编 + 重部 + 验证 NATS 消息流)
