// Package dungeoncmd 承接 DNF 地下城进入、房间移动、拾取和结算类 C2S 协议。
//
// 现有 dnf/dungeon 包只负责 PVF 地下城静态表；本包只做客户端命令入口，
// 后续真实校验和奖励写入必须交给 dungeon/battle owner。
package dungeoncmd
