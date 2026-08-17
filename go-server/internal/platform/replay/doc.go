// Package replay 提供幂等和加密协议流量使用的 nonce 防重放能力。
//
// Memory 后端只适合测试或单节点降级。多 gateway / logic 部署必须使用 Redis 等共享后端，
// 避免同一个 nonce 被另一个进程再次接受。
package replay
