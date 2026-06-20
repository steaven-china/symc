# M9 K8s 真端到端报告(2026-06-20)

> **状态**:✅ 跑通
> **集群**:R720 双 E5-2690 v2 7 节点(3 cp + 4 wk)
> **节点**:k8s-cp-01..03(control-plane)+ k8s-wk-01..04(worker)
> **K8s**:1.31.14 / Calico 3.28 / MetalLB / containerd 1.7.28

## 真端到端信号

```
[18:42:38 INFO]: [symc] Enabling symc v0.1.0
[18:42:38 INFO]: [dev.symc.paper.SymcBootstrap] [symc] NATS connected: nats://nats:4222
[18:42:38 INFO]: [dev.symc.paper.SymcWriteAuthorityManager] [symc] WriteAuthorityManager started for region=paper-region
[18:42:38 INFO]: [dev.symc.paper.SymcCooperationRequest] [symc] CooperationRequest started region=paper-region subject=symc.cooperation.paper-region
[18:42:38 INFO]: [dev.symc.paper.SymcAntiCheatHook] [symc] AntiCheatHook started for region=paper-region
[18:42:38 INFO]: [dev.symc.paper.SymcBootstrap] [symc] runtime started region=paper-region threads=4 nats=nats://nats:4222
[18:42:38 INFO]: [symc] [symc] plugin enabled — region=paper-region
[18:42:44 INFO]: Done (72.182s)! For help, type "help"
```

## 部署架构

- namespace: `symc` (PSA privileged)
- NATS:Deployment + emptyDir + 单节点 JetStream
- Paper:Deployment on k8s-wk-01 + hostPath paper home + base eclipse-temurin:25-jdk
- img-loader:debian pod + ctr for 镜像 import
- containerd mirror:docker.m.daocloud.io via DaemonSet

## 关键设计

| 决策 | 选型 | 原因 |
|---|---|---|
| namespace | `symc` | PSA privileged 允许 NET_ADMIN for netem 注入 |
| NATS 部署 | Deployment + emptyDir + 单节点 | 无 cluster 模式 → 不要 `cluster {}` 块 |
| Paper 部署 | hostPath + base eclipse-temurin | 免 build 镜像 + 免 kaniko |
| Paper nodeName | k8s-wk-01 | pin 到一个 worker |
| 镜像源 | docker.m.daocloud.io | 家用 R720 网络,daocloud 通 library/* + google_containers |
| containerd 配 | DaemonSet + calico 镜像 | 7 节点全配,calico 已 cached |
| plugin jar 部署 | hostPath `/tmp/paper/plugins/` | 免 build,免 registry |
| 镜像导入 | loader pod + debian + ctr | 免 docker,免 SSH |

## 镜像源选型

| 源 | library/* | google_containers/* | 备注 |
|---|---|---|---|
| docker.m.daocloud.io | ✅ | ✅ | **生产用** |
| registry.cn-hangzhou.aliyuncs.com | ❌ | ✅ | K8s 镜像专用 |
| gcr.io/kaniko-project/executor | ❌ | - | kaniko 不通 |

## NATS 单节点 JetStream 配置

```hcl
server_name: nats-0
port: 4222
http_port: 8222
jetstream {
  store_dir: /data
  max_mem: 256M
  max_file: 1G
}
```

**❌ 不要**:`cluster { port: 6222 ... }` 块 — 单节点不要 NATS cluster 模式,会报 "JetStream cluster requires configured routes"

## Paper 部署模式(免 build)

```yaml
# 关键:用 base eclipse-temurin:25-jdk + hostPath /tmp/paper
volumes:
- name: paper-home
  hostPath: { path: /tmp/paper, type: DirectoryOrCreate }
volumeMounts:
- { name: paper-home, mountPath: /opt/paper }
```

**`/tmp/paper/` 内容**:
- `paperclip.jar`(54MB,Paper fork 编译产物)
- `entrypoint.sh`(可选)
- `eula.txt`(`eula=true` 一行)
- `plugins/symc-plugin-0.1.0.jar`(18KB)
- `plugins/symc/config.yml`(**必须先于首次启动!**)
- `world/`(运行时生成)

**预 push 文件**:`tar czf - file1 file2 | kubectl exec -i -n symc img-loader sh -c 'cd /host-tmp && tar xzf -'`

## symc plugin config.yml

**`/tmp/paper/plugins/symc/config.yml`**(首次启动前必须):
```yaml
region-id: paper-region
nats-url: nats://nats:4222
```

**注意**:`saveDefaultConfig` 检测文件存在 → 跳过复制 jar 内默认 config.yml。
**jar 默认 config.yml** 是 `nats://localhost:4222` → 如果 disk 没覆盖,plugin 启不来。

## containerd 配置(7 节点)

```yaml
# 05-containerd-fix.yaml DaemonSet
containers:
- name: fixer
  image: docker.io/calico/node:v3.28.0
  imagePullPolicy: IfNotPresent
  command: ["sh", "-c"]
  args:
    - |
      mkdir -p /host/etc/containerd/certs.d/docker.io
      printf '[host."https://docker.m.daocloud.io"]\n  capabilities = ["pull", "resolve"]\n' > /host/etc/containerd/certs.d/docker.io/hosts.toml
      sleep infinity
  securityContext: { privileged: true }
  volumeMounts: [{ name: host, mountPath: /host }]
volumes: [{ name: host, hostPath: { path: / } }]
```

**config_path 必须改**:
```bash
sed -i 's|config_path = ""|config_path = "/etc/containerd/certs.d"|g' /etc/containerd/config.toml
```

**重启 containerd**:
```bash
# 用 /proc 找(calico 镜像没 ps/chroot/systemctl)
for p in /host/proc/[0-9]*; do
  PID=${p##*/}
  if [ "$(cat $p/comm 2>/dev/null)" = "containerd" ]; then
    kill -9 $PID  # systemd 自动重启
  fi
done
```

## img-loader pod(debian + ctr)

```yaml
# 40-img-loader.yaml
spec:
  nodeName: k8s-wk-01
  containers:
  - name: loader
    image: debian:bookworm-slim
    command: ["sleep", "3600"]
    volumeMounts:
    - { name: host-bin, mountPath: /host-bin }
    - { name: host-tmp, mountPath: /host-tmp }
    - { name: containerd-sock, mountPath: /run/containerd }
  volumes:
  - { name: host-bin, hostPath: { path: /usr/bin } }
  - { name: host-tmp, hostPath: { path: /tmp } }
  - { name: containerd-sock, hostPath: { path: /run/containerd } }
```

## 验证清单(M9 通过)

- [x] `kubectl get pods -n symc` — Paper 1/1 Running, NATS 1/1 Running
- [x] Paper `Done (XX.XXXs)!`
- [x] `[symc] Loading server plugin symc v0.1.0`
- [x] `[symc] Enabling symc v0.1.0`
- [x] `[dev.symc.paper.SymcBootstrap] [symc] NATS connected: nats://nats:4222`
- [x] 3 个 listener 全启
- [x] `[symc] [symc] plugin enabled — region=paper-region`

## 关键文件

- `k8s/00-namespace.yaml` — namespace + PSA labels
- `k8s/05-containerd-fix.yaml` — 7 节点 containerd mirror
- `k8s/10-nats.yaml` — NATS Deployment
- `k8s/20-region.yaml` — Paper Deployment
- `k8s/40-img-loader.yaml` — debian + ctr pod
- `k8s/Dockerfile.paper` — (备用,实际用 hostPath 模式)
- `k8s/entrypoint.sh` — (备用)

## 总结

**symc 完整里程碑路径**:
- M1-M5:sim 实验 + Paper fork 编译
- M6:Java hooks + 自管多线程
- M7:NATS pub/sub
- M8:Paper plugin 跑通(Windows 本地)
- **M9:Paper + symc + NATS 在真 K8s 集群跑通 ✅** ← 今晚

**Learn 累计 8 条 memory** + paper-fork-plugin-m8 skill + K8s 部署模式固化进 memory。
