// Package rpc 负责在内存、本地 Bus 和远端传输端点之间路由内部服务调用。
//
// Endpoint API 会保留请求元数据、追踪上下文、pending 调用上限和 payload 预算。
// Reliable 调用只负责重试临时失败；是否具备持久投递能力取决于配置的 transport / bus 后端。
package rpc
