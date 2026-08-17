# 源码与运行架构

当前结构优先解决两件事：保持 DNF90 业务源码完整，以及把 Windows 本地部署收敛为一个可验证的单机运行单元。第一阶段不对六万行以上、依赖大量同包私有状态的 `dnfbridge` 做一次性目录搬家。

```text
LOGIN.bat ──► DNF90Launcher.exe
                   ├── 注册/认证账号
                   ├── Windows 凭据管理器
                   └── 启动客户端并登记 PID/账号绑定
                                  │
START.bat ────────────────────────┤
                                  ▼
                         DNF90Control.exe
    ├── 校验并解压官方 MySQL 8.4.10 ZIP
    ├── 初始化/启动/停止本包 MySQL
    ├── 生成运行配置与执行预检
    └── 启动 DNF90Server.exe
            │
            ▼
      internal/app/dnf90              唯一装配点
            ├── internal/services/logic/dnf
            │       └── MySQL 权威仓储
            ├── 进程内 worker / event / response queue
            ├── 进程内 cache / lock / presence / bus
            └── internal/services/dnfbridge
                    ├── 协议、会话和封包时序
                    └── internal/modules/dnf/*
```

## 职责

- `cmd/server/*` 只负责进程入口和生命周期。
- `cmd/server/launcher` 是原生 Windows 登录器，负责收集登录信息、调用控制器和保存可选的 Windows 凭据；它不直接访问业务数据库。
- `cmd/server/control` 负责本地安装、配置生成、资产校验、MySQL 与服务进程所有权验证。
- `internal/app/dnf90` 负责组件装配和本地安全停机入口。
- `internal/services/dnfbridge` 负责网络、会话、当前客户端封包结构和时序。
- `internal/modules/dnf/*` 负责可测试的领域规则、数据转换和事务。
- `internal/services/logic/dnf` 负责 MySQL 仓储、区服路由和业务数据持久化。
- `internal/platform` 提供构建闭包实际使用的通用组件。

## 无 Redis 的异步模型

单机模式不需要外部缓存服务才能异步。服务端使用进程内 worker pool、goroutine、channel、事件队列和响应队列，把可以延后执行的工作与网络收发解耦。内存 cache、lock、presence、bus 只服务当前进程，不承担跨进程数据权威。

MySQL 是唯一权威数据源。角色、背包、装备、金币、任务等关键资产必须通过仓储事务同步写入 MySQL；只有提交成功后，业务层才能向客户端确认资产变化。可重建视图和临时队列可以留在内存，重启后从 MySQL 重新装载。

这个边界带来更低的本地部署成本，也明确限制了适用范围：

- 适合单机本地测试；一个 DNF90Server 进程可按登录器登记的客户端 PID 同时承载多个隔离账号；
- 不提供跨节点共享缓存、分布式锁或持久消息队列；
- 进程退出时，尚未完成且未写入 MySQL 的纯内存任务不会跨启动保留；
- 未来若扩展为多节点，应单独引入持久队列或共享协调层，不能把当前内存队列误当成分布式基础设施。

## 文件与数据边界

- `go-server`：只保存可编译源码与源码所需静态资源，不保存运行日志或编译产物。
- `deploy/vendor/mysql/mysql-8.4.10-winx64.zip`：随包交付的官方 MySQL Windows x64 归档。
- `runtime/mysql/server`：首次启动后校验并解压出的 MySQL 程序。
- `runtime/mysql/data`：MySQL 权威数据目录，停止服务不会删除。
- `runtime/config`、`runtime/configs`：本机明文配置与生成配置，不进入源码提交。
- `runtime/state`：安装和进程所有权证据；控制器据此避免终止无关进程。
- `runtime/logs`：MySQL、服务端和协议诊断日志。

后续源码拆分建议按“共享协议值对象 → 会话状态 → PVF 资源目录 → 独立功能 handler”推进。每一步都应有协议测试、当前 EXE 证据和可登录回归，不做一次性目录搬家。
