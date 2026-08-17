package sessioncontract

import (
	"fmt"
	"strings"
)

type PlayerLifecycleState string

const (
	PlayerStateInvalid      PlayerLifecycleState = "invalid"
	PlayerStateLoading      PlayerLifecycleState = "loading"
	PlayerStateOnline       PlayerLifecycleState = "online"
	PlayerStateValid        PlayerLifecycleState = "valid"
	PlayerStateReconnecting PlayerLifecycleState = "reconnecting"
	PlayerStateClosing      PlayerLifecycleState = "closing"
	PlayerStateKicked       PlayerLifecycleState = "kicked"
)

type PlayerLifecycleEvent string

const (
	PlayerEventLoginStart PlayerLifecycleEvent = "login_start"
	PlayerEventLoginBound PlayerLifecycleEvent = "login_bound"
	PlayerEventActivate   PlayerLifecycleEvent = "activate"
	PlayerEventDisconnect PlayerLifecycleEvent = "disconnect"
	PlayerEventKick       PlayerLifecycleEvent = "kick"
	PlayerEventCloseStart PlayerLifecycleEvent = "close_start"
	PlayerEventFlushDone  PlayerLifecycleEvent = "flush_done"
	PlayerEventReclaim    PlayerLifecycleEvent = "reclaim"
	EventRestoreFail      PlayerLifecycleEvent = "restore_after_failure"
)

type PlayerLifecycleTransition struct {
	Event       PlayerLifecycleEvent   `json:"event"`
	From        []PlayerLifecycleState `json:"from"`
	To          PlayerLifecycleState   `json:"to"`
	FlushBefore []string               `json:"flush_before,omitempty"`
	Notes       string                 `json:"notes,omitempty"`
}

var defaultTransitions = []PlayerLifecycleTransition{
	{
		Event: PlayerEventLoginStart,
		From: []PlayerLifecycleState{
			PlayerStateInvalid,
			PlayerStateLoading,
			PlayerStateOnline,
			PlayerStateValid,
			PlayerStateReconnecting,
			PlayerStateClosing,
			PlayerStateKicked,
		},
		To:    PlayerStateLoading,
		Notes: "登录或重连开始后先进入 loading，业务 handler 不应在此阶段执行。",
	},
	{
		Event: PlayerEventLoginBound,
		From:  []PlayerLifecycleState{PlayerStateLoading},
		To:    PlayerStateOnline,
		Notes: "前端 session 绑定完成且 Profile 已加载后进入 online。",
	},
	{
		Event: PlayerEventActivate,
		From:  []PlayerLifecycleState{PlayerStateLoading, PlayerStateOnline, PlayerStateValid},
		To:    PlayerStateValid,
		Notes: "首个玩家作用域 handler 成功后进入 valid，表示可承载状态变更。",
	},
	{
		Event: PlayerEventDisconnect,
		From:  []PlayerLifecycleState{PlayerStateLoading, PlayerStateOnline, PlayerStateValid, PlayerStateReconnecting},
		To:    PlayerStateReconnecting,
		Notes: "断线后先保留 Profile 和 actor，等待同账号重连或 idle 回收。",
	},
	{
		Event: PlayerEventKick,
		From: []PlayerLifecycleState{
			PlayerStateLoading,
			PlayerStateOnline,
			PlayerStateValid,
			PlayerStateReconnecting,
		},
		To:          PlayerStateKicked,
		FlushBefore: []string{"reject_new_packets", "flush_profile", "publish_kick"},
		Notes:       "踢人先阻断新请求，再落盘并发出踢人结果。",
	},
	{
		Event: PlayerEventCloseStart,
		From: []PlayerLifecycleState{
			PlayerStateLoading,
			PlayerStateOnline,
			PlayerStateValid,
			PlayerStateReconnecting,
			PlayerStateKicked,
		},
		To:          PlayerStateClosing,
		FlushBefore: []string{"reject_new_packets", "drain_actor_mailbox", "flush_profile", "close_runtime"},
		Notes:       "服务停机或主动关闭时先进入 closing，等待 actor drain 和 Profile flush。",
	},
	{
		Event:       PlayerEventFlushDone,
		From:        []PlayerLifecycleState{PlayerStateClosing, PlayerStateKicked},
		To:          PlayerStateInvalid,
		FlushBefore: []string{"remove_runtime"},
		Notes:       "Profile 已持久化并卸载后，runtime 才能进入 invalid。",
	},
	{
		Event:       PlayerEventReclaim,
		From:        []PlayerLifecycleState{PlayerStateReconnecting},
		To:          PlayerStateClosing,
		FlushBefore: []string{"reject_new_packets", "flush_profile", "unload_profile", "remove_runtime"},
		Notes:       "断线保留超过 TTL 后触发 idle 回收。",
	},
	{
		Event: EventRestoreFail,
		From:  []PlayerLifecycleState{PlayerStateClosing, PlayerStateInvalid},
		To:    PlayerStateReconnecting,
		Notes: "回收 flush 失败时恢复为 reconnecting，继续等待下一轮回收或重连。",
	},
}

func DefaultPlayerLifecycleTransitions() []PlayerLifecycleTransition {
	return cloneTransitions(defaultTransitions)
}

func cloneTransitions(in []PlayerLifecycleTransition) []PlayerLifecycleTransition {
	out := make([]PlayerLifecycleTransition, len(in))
	for i := range in {
		out[i] = in[i]
		out[i].From = append([]PlayerLifecycleState(nil), in[i].From...)
		out[i].FlushBefore = append([]string(nil), in[i].FlushBefore...)
	}
	return out
}

func NextPlayerLifecycleState(current PlayerLifecycleState, event PlayerLifecycleEvent) (PlayerLifecycleState, error) {
	current = normLifeState(current)
	event = PlayerLifecycleEvent(strings.TrimSpace(string(event)))
	for _, transition := range DefaultPlayerLifecycleTransitions() {
		if transition.Event != event {
			continue
		}
		for _, from := range transition.From {
			if normLifeState(from) == current {
				return transition.To, nil
			}
		}
		return "", fmt.Errorf("player lifecycle event %q cannot move from %q", event, current)
	}
	return "", fmt.Errorf("unknown player lifecycle event %q", event)
}

func ValidatePlayerLifecycleTransition(current PlayerLifecycleState, event PlayerLifecycleEvent) error {
	_, err := NextPlayerLifecycleState(current, event)
	return err
}

func PlayerLifecycleFlushOrder(event PlayerLifecycleEvent) []string {
	event = PlayerLifecycleEvent(strings.TrimSpace(string(event)))
	for _, transition := range DefaultPlayerLifecycleTransitions() {
		if transition.Event != event {
			continue
		}
		return append([]string(nil), transition.FlushBefore...)
	}
	return nil
}

func normLifeState(state PlayerLifecycleState) PlayerLifecycleState {
	trimmed := PlayerLifecycleState(strings.TrimSpace(string(state)))
	switch trimmed {
	case "":
		return PlayerStateInvalid
	case PlayerStateLoading:
		return PlayerStateLoading
	case PlayerStateOnline:
		return PlayerStateOnline
	case PlayerStateValid:
		return PlayerStateValid
	case PlayerStateReconnecting:
		return PlayerStateReconnecting
	case PlayerStateClosing:
		return PlayerStateClosing
	case PlayerStateKicked:
		return PlayerStateKicked
	case PlayerStateInvalid:
		return PlayerStateInvalid
	default:
		return trimmed
	}
}
