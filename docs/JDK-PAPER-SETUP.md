# JDK 25 + Paper fork 准备进度

> **状态**:**JDK 25 装好 + Paper 26.1.2 fork 完整 build 成功**(2026-06-20 23:18,M5 完成)
> **目标**:Paper 26.1.2 浅克隆 → applyPatches 跑通 → fork(dev 分支)→ 准备 M6 集成

---

## 1. 已完成

### 1.1 JDK 25 装好

- **下载**:Adoptium Temurin 25.0.3+9(141MB),用户手动下到 `D:\dls\`
- **复制**:`D:/engine/symc/third-party/jdk25/jdk-25.0.3+9/`
- **版本验证**:`openjdk version "25.0.3" 2026-04-21 LTS` ✅
- **永久环境变量**:`setx JAVA_HOME "D:\engine\symc\third-party\jdk25\jdk-25.0.3+9"`
- **PATH**:`setx PATH "%PATH%;...\bin"`

### 1.2 Gradle 代理配置(关键)

- `Paper/gradle.properties` 追加(避免 curl 的 schannel CONNECT 隧道问题):
  ```properties
  systemProp.http.proxyHost=127.0.0.1
  systemProp.http.proxyPort=7897
  systemProp.https.proxyHost=127.0.0.1
  systemProp.https.proxyPort=7897
  ```

**成功标志**:`paper-server/build/libs/` 出现以下 jar:
- `paper-server-26.1.2.local-SNAPSHOT.jar` (~29MB) — 编译产物
- `paper-bundler-26.1.2.local-SNAPSHOT.jar` (~96MB) — 完整 paperclip 启动器
- `paper-paperclip-26.1.2.local-SNAPSHOT.jar--&lt;hash&gt;`(目录)— 展开的 paperclip

**实际跑通产物(2026-06-20 23:18,12 + 7 = 19 分钟)**:
- `applyPatches`:12m 1s,103 mache + 6 resource + 928 source patches 全部 apply
- `createPaperclipJar`:7m 12s,产出 3 个 jar

### 1.3 .gradle-env.ps1 升级

```powershell
# JDK 25 已装(2026-06-20) — D:/engine/symc/third-party/jdk25/jdk-25.0.3+9
# Paper 26.1.2 需要 JDK 25,这里直接设 JAVA_HOME 给 Paper build 用。
$env:JAVA_HOME = "D:/engine/symc/third-party/jdk25/jdk-25.0.3+9"
$env:GRADLE_USER_HOME = "D:/engine/symc/.gradle"
$env:GRADLE_OPTS = "-Xmx2g -Dfile.encoding=UTF-8"
```

### 1.4 Gradle wrapper 9.4.1 下载成功

- Gradle 9.4.1 + Java 26 support(虽然我们用 25,向下兼容)
- 提示:`./gradlew.bat applyPatches` 任务已识别

---

## 2. applyPatches 进度(未完成)

**bg_9 跑了 509 秒 (~8.5 分钟),bg_8 跑了 224 秒,两次都 exit 1 卡在 "Applying mache patches"**。

**已走完的阶段**:
- ✅ Gradle 9.4.1 wrapper 下载
- ✅ `downloadMcManifest` / `downloadServerJar` — MC 1.26.1 server jar 下完
- ✅ `macheDecompileJar` — VineFlower 反编译完成
- ❌ `Applying mache patches` — 卡死,具体错误未捕获

**Mojang 反编译输出在 `paper-server/build/tmp/macheDecompileJar/`**(部分,但已被我误删 — 详见 §5.1)。

**未生成**:
- `paper-server/src/minecraft/`(MC 反编译源) — 0 文件
- `paper-server/build/libs/paperclip.jar`(最终产物) — 0 文件

---

## 3. 用户手动跑命令(推荐)

```powershell
# 在 D:/engine/symc 下,PowerShell 终端
cd D:\engine\symc\Paper
. ..\.gradle-env.ps1
.\gradlew.bat applyPatches --stacktrace
```

**首次跑预期 30 分钟**(代理慢 + MC 反编译重做)。**不退出终端**,让它跑完。

**如果 OOM 报错**:
```powershell
# 增大堆到 6g
$env:GRADLE_OPTS = "-Xmx6g -Dfile.encoding=UTF-8"
.\gradlew.bat applyPatches --stacktrace
```

**如果网络/代理问题**:
- 确认 `Paper/gradle.properties` 的 `systemProp.*.proxyHost/Port` 还在(可能被覆盖)
- 临时设 `$env:HTTP_PROXY` / `$env:HTTPS_PROXY = "http://127.0.0.1:7897"` 在跑前

**成功后**:`paper-server/src/minecraft/` 有 MC 1.26.1 全部源,`paper-server/build/libs/paperclip.jar` 生成。

---

## 4. 待办(applyPatches 成功后)

### 4.1 验证

- `ls paper-server/src/minecraft/net/minecraft/` — MC 源码展开
- `ls paper-server/build/libs/` — paperclip.jar
- `.\gradlew.bat runServer` — 起 Paper 一次(确认 patch 没破)

### 4.2 Paper fork 升级

浅克隆 → 加 remote → 建 dev 分支:
```bash
cd D:/engine/symc/Paper
git remote rename origin upstream
git remote add origin https://github.com/<user>/symc-fork.git
git checkout -b dev
git push -u origin dev
```

### 4.3 M6 准备

加 symc 特定 patches:
- `SymcWriteAuthorityManager.java`(D2 写权漂移)
- `SymcCooperationRequest.java`(D-extra 协议)
- `SymcAntiCheatHook.java`(D4 反作弊)
- `config/symc.toml` 配置

---

## 5. 已知问题

### 5.1 清理误删缓存(2026-06-20,我犯的错)

清理 `taskkill java.exe` 时**误删了** `Paper/build/` 和 `paper-server/src/` — 这些**包含 MC 1.26.1 jar 下载和部分反编译输出**。

**用户首次手动跑 applyPatches** 会**重下 MC jar (~30MB,2 分钟通过代理) + 重做反编译 (~5 分钟)**。不致命,但多等几分钟。

**下次会话别再犯**:
- 清理前先看 `Paper/build/` 里**有没有** `macheDecompileJar/` / `tmp/` 等大目录(缓存**保留**)
- 只清 `Paper/.gradle/daemon/*/registry.bin` + `taskkill java.exe` 就够

### 5.2 Lock 冲突

之前 2 个后台任务同时跑 `applyPatches`,Gradle wrapper 报"Timeout 120000 waiting for exclusive access to file"。修法:跑前 `taskkill /F /IM java.exe` 清掉残留。

### 5.3 代理配置对比

- ❌ `setx HTTP_PROXY` 给 curl 用:schannel CONNECT 隧道对大文件不稳
- ❌ `$env:HTTPS_PROXY` 给 PowerShell:同样 schannel 问题
- ❌ `Invoke-WebRequest -Proxy`:Microsoft .NET 的 WebException 中文乱码不好诊断
- ✅ **`gradle.properties` 配 `systemProp.*.proxy*`**:gradle 自己用 Java HttpClient,稳定

### 5.4 中文 Windows 编码

`taskkill` / `setx` 输出中文乱码("成功: 终止进程..."),但**功能正常**,只要 exit code 0 即可信。

---

## 6. 经验

1. **Windows 大文件下载用 PowerShell 或直接手动** — curl + schannel + proxy 不靠谱
2. **Gradle 代理**:`gradle.properties` 的 `systemProp.*` 比环境变量可靠
3. **`setx` 改用户环境变量当前 shell 不生效**,新 shell 才生效 — `.gradle-env.ps1` 直接 export 绕过
4. **Paper 浅克隆不拉 LFS**(我之前误以为 LFS 问题),实际 patches/ 在 `paper-server/patches/` 子目录
5. **lock 冲突是 Gradle wrapper 常见坑** — 跑前必清 java 进程
6. **别误删 `Paper/build/`** — 含缓存,删了下次重下

---

*本文档是 2026-06-20 P2(JDK 25 + Paper fork 准备)中间产物。applyPatches 完成情况由用户手动跑后补全。*
