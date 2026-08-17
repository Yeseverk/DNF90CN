// Package protocol 收口 DNF 最新客户端的服务端网络包格式。
//
// 本包只负责协议外壳：校验、拆包和封包。
// 频道目录来自 channelcatalog，玩家可变数据来自 repository，不能在这里读取 PVF 或访问数据库。
package protocol
