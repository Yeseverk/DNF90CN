// Package staticdata 负责 DNF PVF 静态数据的一次性内存装配。
//
// 本包只组合项目层 PVF 索引和强类型表，不直接访问磁盘、不写 Profile、
// 不发奖励，也不修改任何玩家状态。
package staticdata
