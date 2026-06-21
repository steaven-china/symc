# M10 真 listener 验证报告(2026-06-21)

> **状态**:✅ 跑通
> **验证**:NATS connz 显示 symc plugin 真 subscribe 了 `symc.cooperation.region-a`

## 真端到端信号(M10)

```json
// NATS connz?subs=true 输出
{
  "num_connections": 1,
  "connections": [
    {
      "cid": 6,
      "kind": "Client",
      "type": "nats",
      "ip": "192.168.182.151",  // paper-region pod IP
      "lang": "java",
      "version": "2.17.0",        // jnats client
      "subscriptions": 1,
      "subscriptions_list": [
        "symc.cooperation.region-a"  // ← 真的订阅了
      ]
    }
  ]
}
```

```log
// Paper pod 日志(02:27:08-09)
[symc] [symc] config: region=region-a nats=nats://nats:4222
[dev.symc.paper.SymcBootstrap] [symc] NATS connected: nats://nats:4222
[dev.symc.paper.SymcCooperationRequest] [symc] CooperationRequest started region=region-a subject=symc.cooperation.region-a
[symc] [symc] plugin enabled: region=region-a nats=nats://nats:4222
```

## M10 关键改动

| 文件 | 改动 |
|---|---|
| `paper-server/src/main/java/dev/symc/paper/SymcPlugin.java` | 改 `getString("nats-url", "nats://nats:4222")` 默认值(K8s service 名)+ env var 优先读 `REGION` + `NATS_URL` |
| `paper-server/src/main/java/dev/symc/paper/SymcCooperationRequest.java` | 改 `LOG.fine/warning`(slf4j 不支持)→ `LOG.debug/warn` + 升级 `handleIncoming` 到 INFO + 真 `fromJson` 解析(JSON extract 极简版) |
| `plugins/symc-plugin-0.1.0.jar` | 重 build,18768 bytes(18633 → 18768,+135 字节 env var + JSON 解析) |

## K8s hostPath 部署

- Paper pod 在 `k8s-wk-01` (nodeName pinned)
- `hostPath: /tmp/paper`(paper home)
- 内部:`/opt/paper/paperclip.jar` + `plugins/symc-plugin-0.1.0.jar` + `plugins/symc/config.yml` + `eula.txt`
- `jar-updater` pod(debian + hostPath)用于从 Windows 推新 jar
- base64 流式传输(md5 校验)替代 tar(避免管道二进制污染)

## 关键命令

```bash
# 1. 恢复 Java 源(git checkout)
cd /d/engine/symc/Paper
git checkout 4d39a73 -- paper-server/src/main/java/io/papermc/paper/symc/
mkdir -p paper-server/src/main/java/dev/symc/paper
mv paper-server/src/main/java/io/papermc/paper/symc/*.java paper-server/src/main/java/dev/symc/paper/
sed -i 's|^package io.papermc.paper.symc;|package dev.symc.paper;|' paper-server/src/main/java/dev/symc/paper/*.java

# 2. 编辑(SymcPlugin + SymcCooperationRequest)
# ... 改 nats-url default + logger 改 + JSON parser

# 3. 重 build
.\gradlew.bat :paper-server:symcPluginJar --no-daemon

# 4. base64 推到 K8s hostPath
base64 -w0 plugins/symc-plugin-0.1.0.jar | \
  kubectl exec -i -n symc jar-updater -- sh -c \
  'base64 -d > /opt/paper/plugins/symc-plugin-0.1.0.jar && md5sum /opt/paper/plugins/symc-plugin-0.1.0.jar'

# 5. restart Paper
kubectl delete pod -n symc -l app=paper-region

# 6. 验证 NATS subscribe
kubectl exec -n symc jar-updater -- curl -s 'http://nats:8222/connz?subs=true' | grep symc
```

## 验证 M10 通过的标志

- ✅ Paper pod 启动 + `[symc] config: region=region-a nats=nats://nats:4222`
- ✅ `NATS connected: nats://nats:4222`(不再 localhost)
- ✅ `CooperationRequest started subject=symc.cooperation.region-a`
- ✅ `plugin enabled: region=region-a nats=nats://nats:4222`
- ✅ `Paper Done (72.085s)`
- ✅ NATS `connz` 显示 `subscriptions_list: ["symc.cooperation.region-a"]`
- ✅ jnats 2.17.0 java client 真的连上

## 已知限制

- ❌ **0 消息流过**(`in_msgs: 0, out_msgs: 0`)—— 还没触发红石事件 + 测试 publish
- ❌ **M10 publisher 没真测试**——需要 1 个真红石事件触发 `onRedstone()` 才算完全验证
- ❌ **NATS nc publish 失败**("Unknown Protocol Operation")—— Python script 该能修,但 nats-box 镜像 daocloud 未缓存
- ❌ **NATS HTTP API** 2.10 `/pubz` 返回 404——需要看下 NATS 2.10 实际 API 路径
- ❌ **SymcWriteAuthorityManager + SymcAntiCheatHook 仍是 stub**——M10 只改 CooperationRequest

## 下一步(M11+)

- M11:在 Paper pod 内 `setblock` 触发红石事件 → `onRedstone()` → NATS publish
- M11:实现真 WriteAuthorityManager(双 region 写权冲突检测)
- M11:scale 2nd region
- M12:真 AntiCheatHook(SHA256)
- M13:headlessmc 1000 bots 压测

## Learn 累计

- 8 条 memory 在 symc 项目
- 1 managed skill(paper-fork-plugin-m8)
- 1 K8s deployment 完整模式
