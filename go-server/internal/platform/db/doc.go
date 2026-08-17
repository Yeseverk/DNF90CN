// Package db 提供 Profile、读模型、事件和 runtime 状态模块共用的存储契约。
//
// 这里刻意拆开克隆、字段级脏标记、异步刷盘、重试和死信处理，让玩法模块在不改
// mutation 代码的前提下切换 memory、Redis、SQL 或 Mongo 后端。
package db
