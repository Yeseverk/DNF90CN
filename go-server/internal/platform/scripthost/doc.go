// Package scripthost 定义脚本运行时接入边界。
//
// Host 和 Program 实现必须可被并发调用，并且必须尊重传入 context。
// 适配层应把脚本 panic、VM trap、超时和资源限额命中转换为 error；框架不把脚本崩溃视为可忽略事件。
// Program.Execute 不得复用调用方可变输入缓冲区作为长期状态，返回的 Result 也应由实现方完成拷贝隔离。
// Runtime 只负责脚本版本注册、预检、切换和执行；Profile 写入、事务、EventLog 和 Outbox 仍由 Go 侧可靠链路执行。
package scripthost
