// Package party 负责当前 DNF 客户端已经闭合的队伍请求和实时成员状态。
//
// 当前 NoPack.exe 将 class0/op153 解析为队伍实时成员状态。class0/op9 属于城镇场景记录，
// 不能复用旧服务端的队伍快照布局。
package party
