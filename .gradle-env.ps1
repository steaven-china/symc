# 把 Gradle 用户目录(缓存、依赖、wrapper)重定向到 D 盘,
# 默认在 C:/Users/<you>/.gradle,跑 Paper build 会塞一堆东西到 C 盘。
#
# 用法(在 D:/engine/symc 下):
#   . .\.gradle-env.ps1
# 然后:
#   cd Paper
#   .\gradlew.bat applyPatches
#
# JDK 25 已装(2026-06-20) — D:/engine/symc/third-party/jdk25/jdk-25.0.3+9
# Paper 26.1.2 需要 JDK 25,这里直接设 JAVA_HOME 给 Paper build 用。
$env:JAVA_HOME = "D:/engine/symc/third-party/jdk25/jdk-25.0.3+9"
$env:GRADLE_USER_HOME = "D:/engine/symc/.gradle"
$env:GRADLE_OPTS = "-Xmx2g -Dfile.encoding=UTF-8"