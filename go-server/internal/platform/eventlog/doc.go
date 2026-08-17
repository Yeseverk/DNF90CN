// Package eventlog 记录可持久化的业务事件，并将它们重放给下游投影。
//
// EventLog 是幂等命令、读模型重建、审计和 runtime 集成的恢复主线。调用方应把
// append 成功视为命令对系统其他部分可见的边界。
package eventlog
