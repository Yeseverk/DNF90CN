# DNF90CN 开发环境

## 进入 90CN

### 客户端

将以下五个文件替换到客户端根目录：

- `DNF.exe`
- `90CN.dll`
- `90CNLua.dll`
- `ijl15.dll`
- `ijl15_real.dll`

### 服务端

分发给玩家或测试人员时，**必须**使用 `PACKAGE_RELEASE.bat` 生成的 ZIP。`runtime\bin` 被 Git 忽略，直接复制源码仓库得到的目录不包含四个运行时 EXE，玩家那边一定跑不起来。

发布包里 `runtime\bin\DNF90Build.version` 与 `deploy\windows\runtime.version` 一致，所以玩家双击 `LOGIN.bat` 会直接弹出登录框，不会出现黑框提示。若安装目录里的运行时与源码版本不一致且本机没有装 Go，`LOGIN.bat` 会明确拒绝启动并给出恢复方式——它不会给旧 EXE 补版本标记，因为那会让陈旧的服务端被当成最新版本一直跑下去。

1. 双击运行 `LOGIN.bat`。
2. 首次运行时选择客户端目录中的 `DNF.exe`。
3. 新账号点击“注册并进入”；已有账号点击“进入游戏”。

服务端开发交接文档见 `go-server/docs/PROJECT_HANDOFF.md`。
