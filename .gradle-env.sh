# 把 Gradle 用户目录(缓存、依赖、wrapper)重定向到 D 盘,
# 默认在 C:/Users/<you>/.gradle,跑 Paper build 会塞一堆东西到 C 盘。
#
# 用法(在 D:/engine/symc 下,PowerShell):
#   . .\.gradle-env.ps1
# 然后:
#   cd Paper
#   .\gradlew.bat applyPatches
#
# 注意:Paper 26.1.2 需要 JDK 25。本机当前是 JDK 21,build 会报 toolchain 错。
# 要么装 JDK 25 后 JAVA_HOME 指过去,要么别跑 build。
export GRADLE_USER_HOME="D:/engine/symc/.gradle"
export GRADLE_OPTS="-Xmx2g -Dfile.encoding=UTF-8"