// 本文件解析任务类 C2S 请求体。
// C# QuestManager 会在 body 长度大于 2 时去掉前 2 字节 wire-type echo，再交给 QuestService。
package quest

import (
	"encoding/binary"
	"fmt"

	"longheng.io/server/internal/modules/dnf/dnfenum"
)

const (
	CurrentFinishQuestRequestBodySize    = 10
	CurrentFinishQuestObservedTailMarker = ^uint16(0)
)

// QuestIDRequest 描述只携带 questId 的任务请求。
type QuestIDRequest struct {
	QuestID uint16
}

// SetTriggerRequest 描述任务触发进度请求。
type SetTriggerRequest struct {
	QuestID     uint16
	TriggerType byte
	IsIncrement bool
}

// FinishQuestRequest 描述完成任务请求。
type FinishQuestRequest struct {
	QuestID           uint16
	RewardSelectIndex uint16
	HasRewardSelect   bool
	Multiplier        uint16
	Reserved          uint16
}

// DecodeQuestIDRequest 解析接取/放弃任务请求。
func DecodeQuestIDRequest(body []byte) (QuestIDRequest, error) {
	qBody := stripEcho(body)
	if len(qBody) < 2 {
		return QuestIDRequest{}, fmt.Errorf("body too short after echo strip: got %d want >= 2", len(qBody))
	}
	return QuestIDRequest{QuestID: binary.LittleEndian.Uint16(qBody)}, nil
}

// DecodeSetTriggerRequest 解析任务触发请求。
func DecodeSetTriggerRequest(body []byte) (SetTriggerRequest, error) {
	qBody := stripEcho(body)
	if len(qBody) < 3 {
		return SetTriggerRequest{}, fmt.Errorf("body too short after echo strip: got %d want >= 3", len(qBody))
	}
	return SetTriggerRequest{
		QuestID:     binary.LittleEndian.Uint16(qBody),
		TriggerType: qBody[2],
		IsIncrement: len(qBody) >= 4 && qBody[3] != 0,
	}, nil
}

// DecodeFinishQuestRequest 解析完成任务请求。
func DecodeFinishQuestRequest(body []byte) (FinishQuestRequest, error) {
	if len(body) != CurrentFinishQuestRequestBodySize {
		return FinishQuestRequest{}, fmt.Errorf("current EXE finish-quest body size: got %d want %d", len(body), CurrentFinishQuestRequestBodySize)
	}
	if echo := binary.LittleEndian.Uint16(body[0:2]); echo != uint16(dnfenum.CmdPacketFinishQuest) {
		return FinishQuestRequest{}, fmt.Errorf("current EXE finish-quest echo: got %d want %d", echo, dnfenum.CmdPacketFinishQuest)
	}
	req := FinishQuestRequest{
		QuestID:           binary.LittleEndian.Uint16(body[2:4]),
		RewardSelectIndex: binary.LittleEndian.Uint16(body[4:6]),
		Multiplier:        binary.LittleEndian.Uint16(body[6:8]),
		Reserved:          binary.LittleEndian.Uint16(body[8:10]),
	}
	req.HasRewardSelect = req.RewardSelectIndex != ^uint16(0)
	return req, nil
}

func stripEcho(body []byte) []byte {
	if body == nil || len(body) <= 2 {
		return body
	}
	return body[2:]
}

func (r QuestIDRequest) String() string {
	return fmt.Sprintf("quest=%d", r.QuestID)
}

func (r SetTriggerRequest) String() string {
	return fmt.Sprintf("quest=%d triggerType=%d increment=%t", r.QuestID, r.TriggerType, r.IsIncrement)
}

func (r FinishQuestRequest) String() string {
	return fmt.Sprintf("quest=%d rewardSelect=%d hasReward=%t multiplier=%d reserved=%d", r.QuestID, r.RewardSelectIndex, r.HasRewardSelect, r.Multiplier, r.Reserved)
}
