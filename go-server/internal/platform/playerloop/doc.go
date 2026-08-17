// Package playerloop 将单个玩家的工作串行化到账号维度队列。
//
// 这样可以保护 Profile 和玩法状态不被并发修改，同时允许不同账号并行处理。Handler
// 不能无限阻塞，否则会卡住该玩家自己的队列。
package playerloop
