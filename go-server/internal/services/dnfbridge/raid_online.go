// raid_online.go 负责当前 game 连接内的攻坚队运行态流程。
// 这里只保存在线会话可见的短生命周期攻坚队，不落 DB；跨服持久 owner 后续再替换该边界。
package dnfbridge

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	"longheng.io/server/internal/modules/dnf/raid"
)

const (
	runtimeRaidDefaultGroup = 1
	runtimeRaidMaxGroups    = 5
	runtimeRaidGroupSize    = 4
	runtimeRaidFailureCode  = 3
)

type runtimeRaidState struct {
	RaidKey         uint32
	LeaderID        uint16
	RouteOrRaidType byte
	NameBytes       []byte
	TailFlag        byte
	Members         []runtimeRaidMemberState
	Started         bool
}

type runtimeRaidMemberState struct {
	CharID     uint16
	Name       string
	GroupIndex byte
	SlotOrder  byte
	UserState  byte
	HPPercent  byte
	MPPercent  byte
}

func (s *Service) handleOnlineRaidCommand(session *gameSession, typ uint16, body []byte) (bool, error) {
	switch dnfenum.CmdPacket(typ) {
	case dnfenum.CmdPacketCreateRaid:
		return true, s.handleOnlineCreateRaid(session, typ, body)
	case dnfenum.CmdPacketModifyRaidInfo:
		return true, s.handleOnlineModifyRaidInfo(session, typ, body)
	case dnfenum.CmdPacketLeaveRaid:
		return true, s.handleOnlineLeaveRaid(session, typ, body)
	case dnfenum.CmdPacketStartRaid:
		return true, s.handleOnlineStartRaid(session, typ, body)
	case dnfenum.CmdPacketRejoinRaid:
		return true, s.handleOnlineRejoinRaid(session, typ, body)
	case dnfenum.CmdPacketRaidManagerWork:
		return true, s.handleOnlineRaidManagerWork(session, typ, body)
	case dnfenum.CmdPacketRaidMemberChangeState:
		return true, s.handleOnlineRaidMemberChangeState(session, typ, body)
	default:
		return false, nil
	}
}

func (s *Service) handleOnlineCreateRaid(session *gameSession, typ uint16, body []byte) error {
	req, err := raid.DecodeCreateRaidRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-raid-create-body-invalid", "type", typ, "body_len", len(body), "error", err)
		return s.sendRaidCommandFailure(session, typ)
	}
	sourceID := selectedCharacterID(session)
	if sourceID == 0 {
		return s.sendRaidCommandFailure(session, typ)
	}
	state := s.createRuntimeRaid(session, sourceID, req)
	if err := s.sendGameUpperRawClassCodec(session, typ, raid.BuildCreateRaidResultBody(state.RaidKey), dnfproto.DefaultChannelClassification, true); err != nil {
		return err
	}
	if err := s.broadcastRuntimeRaidRefresh(state); err != nil {
		return err
	}
	s.logGameEvent(session, "game-raid-created",
		"type", typ,
		"raid_key", state.RaidKey,
		"leader_id", state.LeaderID,
		"member_count", len(state.Members))
	return nil
}

func (s *Service) handleOnlineModifyRaidInfo(session *gameSession, typ uint16, body []byte) error {
	req, err := raid.DecodeModifyRaidInfoRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-raid-modify-body-invalid", "type", typ, "body_len", len(body), "error", err)
		return s.sendRaidCommandFailure(session, typ)
	}
	sourceID := selectedCharacterID(session)
	state, ok := s.modifyRuntimeRaidInfo(sourceID, req)
	if !ok {
		return s.sendRaidCommandFailure(session, typ)
	}
	if err := s.sendGameUpperRawClassCodec(session, typ, []byte{1}, dnfproto.DefaultChannelClassification, true); err != nil {
		return err
	}
	return s.broadcastRuntimeRaidRefresh(state)
}

func (s *Service) handleOnlineLeaveRaid(session *gameSession, typ uint16, body []byte) error {
	if _, err := raid.DecodeLeaveRaidRequest(body); err != nil {
		s.logGameEvent(session, "game-raid-leave-body-invalid", "type", typ, "body_len", len(body), "error", err)
		return s.sendRaidCommandFailure(session, typ)
	}
	sourceID := selectedCharacterID(session)
	state, removed, disbanded := s.removeRuntimeRaidMember(sourceID)
	if !removed {
		return s.sendRaidCommandFailure(session, typ)
	}
	if err := s.sendGameUpperRawClassCodec(session, typ, raid.BuildLeaveRaidResultBody(disbanded), dnfproto.DefaultChannelClassification, true); err != nil {
		return err
	}
	storeRuntimePartyState(session, alignedcmd.PartyState{})
	if err := s.sendRuntimePartyMemberRemoved(session, sourceID, alignedcmd.PartyState{}); err != nil {
		return err
	}
	if disbanded {
		return nil
	}
	return s.broadcastRuntimeRaidRefresh(state)
}

func (s *Service) handleOnlineStartRaid(session *gameSession, typ uint16, body []byte) error {
	if err := raid.DecodeStartRaidRequest(body); err != nil {
		s.logGameEvent(session, "game-raid-start-body-invalid", "type", typ, "body_len", len(body), "error", err)
		return s.sendRaidCommandFailure(session, typ)
	}
	sourceID := selectedCharacterID(session)
	state, ok := s.markRuntimeRaidStarted(sourceID)
	if !ok {
		return s.sendRaidCommandFailure(session, typ)
	}
	if err := s.sendGameUpperRawClassCodec(session, typ, []byte{1}, dnfproto.DefaultChannelClassification, true); err != nil {
		return err
	}
	if err := s.broadcastRuntimeRaidRefresh(state); err != nil {
		return err
	}
	if err := s.applyRuntimeRaidParties(state); err != nil {
		return err
	}
	s.logGameEvent(session, "game-raid-started",
		"type", typ,
		"raid_key", state.RaidKey,
		"member_count", len(state.Members))
	return nil
}

func (s *Service) handleOnlineRejoinRaid(session *gameSession, typ uint16, body []byte) error {
	req, err := raid.DecodeRejoinRaidRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-raid-rejoin-body-invalid", "type", typ, "body_len", len(body), "error", err)
		return s.sendRaidCommandFailure(session, typ)
	}
	sourceID := selectedCharacterID(session)
	state, ok := s.rejoinRuntimeRaid(req.RaidKey, sourceID)
	if !ok {
		return s.sendRaidCommandFailure(session, typ)
	}
	if err := s.sendGameUpperRawClassCodec(session, typ, []byte{1}, dnfproto.DefaultChannelClassification, true); err != nil {
		return err
	}
	return s.broadcastRuntimeRaidRefresh(state)
}

func (s *Service) handleOnlineRaidManagerWork(session *gameSession, typ uint16, body []byte) error {
	req, err := raid.DecodeManagerWorkRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-raid-manager-body-invalid", "type", typ, "body_len", len(body), "error", err)
		return nil
	}
	sourceID := selectedCharacterID(session)
	state, ok := s.moveRuntimeRaidMember(sourceID, uint16(req.MemberCharKey), byte(req.TargetGroup))
	if !ok {
		s.logGameEvent(session, "game-raid-manager-rejected",
			"type", typ,
			"source_char_id", sourceID,
			"member_char_id", req.MemberCharKey,
			"target_group", req.TargetGroup)
		return nil
	}
	// MCP/IDA 证据显示 S2C 669 是 DoNothing；只推 0x24F mode=3 刷新 roster。
	return s.broadcastRuntimeRaidRefresh(state)
}

func (s *Service) handleOnlineRaidMemberChangeState(session *gameSession, typ uint16, body []byte) error {
	req, err := raid.DecodeMemberChangeStateRequest(body)
	if err != nil {
		s.logGameEvent(session, "game-raid-member-state-body-invalid", "type", typ, "body_len", len(body), "error", err)
		return nil
	}
	sourceID := selectedCharacterID(session)
	state, ok := s.changeRuntimeRaidMemberState(sourceID, normalizeRuntimeRaidMemberState(req.State))
	if !ok {
		s.logGameEvent(session, "game-raid-member-state-ignored",
			"type", typ,
			"source_char_id", sourceID,
			"state", req.State)
		return nil
	}
	// MCP/IDA 证据显示 823 是客户端成员状态变化；可见同步走 0x24F 成员名单刷新。
	return s.broadcastRuntimeRaidRefresh(state)
}

func normalizeRuntimeRaidMemberState(state byte) byte {
	if state == 0 {
		return 0
	}
	return 1
}

func (s *Service) createRuntimeRaid(session *gameSession, sourceID uint16, req raid.InfoRequest) runtimeRaidState {
	s.raidMu.Lock()
	defer s.raidMu.Unlock()
	s.ensureRuntimeRaidMapsLocked()
	s.removeRuntimeRaidMemberLocked(sourceID)
	key := s.nextRuntimeRaidKeyLocked(session, sourceID)
	state := runtimeRaidState{
		RaidKey:         key,
		LeaderID:        sourceID,
		RouteOrRaidType: req.RouteOrRaidType,
		NameBytes:       append([]byte(nil), req.NameBytes...),
		TailFlag:        req.TailFlag,
	}
	state.Members = s.initialRuntimeRaidMembersLocked(session, sourceID)
	s.raids[key] = &state
	return cloneRuntimeRaidState(state)
}

func (s *Service) modifyRuntimeRaidInfo(sourceID uint16, req raid.InfoRequest) (runtimeRaidState, bool) {
	s.raidMu.Lock()
	defer s.raidMu.Unlock()
	state := s.runtimeRaidByMemberLocked(sourceID)
	if state == nil || state.LeaderID != sourceID {
		return runtimeRaidState{}, false
	}
	state.RouteOrRaidType = req.RouteOrRaidType
	state.NameBytes = append(state.NameBytes[:0], req.NameBytes...)
	state.TailFlag = req.TailFlag
	return cloneRuntimeRaidState(*state), true
}

func (s *Service) removeRuntimeRaidMember(sourceID uint16) (runtimeRaidState, bool, bool) {
	s.raidMu.Lock()
	defer s.raidMu.Unlock()
	state, removed, disbanded := s.removeRuntimeRaidMemberLocked(sourceID)
	if state == nil {
		return runtimeRaidState{}, removed, disbanded
	}
	return cloneRuntimeRaidState(*state), removed, disbanded
}

func (s *Service) markRuntimeRaidStarted(sourceID uint16) (runtimeRaidState, bool) {
	s.raidMu.Lock()
	defer s.raidMu.Unlock()
	state := s.runtimeRaidByMemberLocked(sourceID)
	if state == nil || state.LeaderID != sourceID || len(state.Members) == 0 {
		return runtimeRaidState{}, false
	}
	if runtimeRaidHasOversizedGroup(*state) {
		return runtimeRaidState{}, false
	}
	state.Started = true
	return cloneRuntimeRaidState(*state), true
}

func (s *Service) rejoinRuntimeRaid(raidKey uint32, sourceID uint16) (runtimeRaidState, bool) {
	if sourceID == 0 {
		return runtimeRaidState{}, false
	}
	s.raidMu.Lock()
	defer s.raidMu.Unlock()
	state := s.raids[raidKey]
	if state == nil {
		return runtimeRaidState{}, false
	}
	if runtimeRaidMemberIndex(*state, sourceID) < 0 {
		if len(state.Members) >= raid.MaxAttackPartyMembers {
			return runtimeRaidState{}, false
		}
		state.Members = append(state.Members, s.newRuntimeRaidMemberLocked(sourceID, runtimeRaidDefaultGroup, nextRuntimeRaidSlot(*state, runtimeRaidDefaultGroup)))
	}
	return cloneRuntimeRaidState(*state), true
}

func (s *Service) changeRuntimeRaidMemberState(sourceID uint16, userState byte) (runtimeRaidState, bool) {
	if sourceID == 0 {
		return runtimeRaidState{}, false
	}
	s.raidMu.Lock()
	defer s.raidMu.Unlock()
	state := s.runtimeRaidByMemberLocked(sourceID)
	if state == nil {
		return runtimeRaidState{}, false
	}
	index := runtimeRaidMemberIndex(*state, sourceID)
	if index < 0 {
		return runtimeRaidState{}, false
	}
	state.Members[index].UserState = userState
	return cloneRuntimeRaidState(*state), true
}

func (s *Service) moveRuntimeRaidMember(sourceID uint16, memberID uint16, targetGroup byte) (runtimeRaidState, bool) {
	if targetGroup == 0 || targetGroup > runtimeRaidMaxGroups {
		return runtimeRaidState{}, false
	}
	s.raidMu.Lock()
	defer s.raidMu.Unlock()
	state := s.runtimeRaidByMemberLocked(sourceID)
	if state == nil || state.LeaderID != sourceID {
		return runtimeRaidState{}, false
	}
	index := runtimeRaidMemberIndex(*state, memberID)
	if index < 0 {
		return runtimeRaidState{}, false
	}
	state.Members[index].GroupIndex = targetGroup
	state.Members[index].SlotOrder = nextRuntimeRaidSlotExcluding(*state, targetGroup, memberID)
	runtimeRaidSortMembers(state.Members)
	return cloneRuntimeRaidState(*state), true
}

func (s *Service) broadcastRuntimeRaidRefresh(state runtimeRaidState) error {
	body, err := raid.BuildMemberRefreshMode3Body(runtimeRaidMemberRefresh(state))
	if err != nil {
		return err
	}
	for _, member := range state.Members {
		session, ok := s.onlineGameSession(member.CharID)
		if !ok {
			continue
		}
		if err := s.sendGameUpperRawClassCodec(session, raid.RaidMemberRefreshMsgID, body, 0, true); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) applyRuntimeRaidParties(state runtimeRaidState) error {
	groupStates := make([]alignedcmd.PartyState, 0)
	for _, group := range runtimeRaidPartyGroups(state) {
		if len(group) == 0 {
			continue
		}
		partyState, replaced := s.replaceManagedRuntimeParty(runtimeRaidGroupPartyState(state, group))
		if !replaced {
			return fmt.Errorf("raid party group could not be applied through runtime party manager")
		}
		groupStates = append(groupStates, partyState)
	}
	// Any prior ordinary party that still has non-raid survivors was changed by
	// the central manager while group members moved. Refresh its projections
	// from manager snapshots before writing any client-facing raid roster.
	s.publishAllManagedRuntimeParties()
	for _, partyState := range groupStates {
		for _, member := range runtimePartyMembers(partyState) {
			session, ok := s.onlineGameSession(member.UserID)
			if !ok {
				continue
			}
			if err := s.sendRuntimePartySnapshot(session, partyState); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) initialRuntimeRaidMembersLocked(session *gameSession, sourceID uint16) []runtimeRaidMemberState {
	members := []runtimeRaidMemberState{s.newRuntimeRaidMemberLocked(sourceID, runtimeRaidDefaultGroup, 0)}
	state := runtimePartyStateSnapshot(session)
	for _, member := range runtimePartyMembers(state) {
		if member.UserID == 0 || member.UserID == sourceID || len(members) >= raid.MaxAttackPartyMembers {
			continue
		}
		members = append(members, s.newRuntimeRaidMemberLocked(member.UserID, runtimeRaidDefaultGroup, byte(len(members)%runtimeRaidGroupSize)))
	}
	return members
}

func (s *Service) newRuntimeRaidMemberLocked(charID uint16, group byte, slot byte) runtimeRaidMemberState {
	return runtimeRaidMemberState{
		CharID:     charID,
		Name:       s.runtimeRaidCharacterName(charID),
		GroupIndex: group,
		SlotOrder:  slot,
		UserState:  1,
		HPPercent:  100,
		MPPercent:  100,
	}
}

func (s *Service) runtimeRaidCharacterName(charID uint16) string {
	repos, ok := s.repositoryGroup()
	if ok && repos.Character != nil {
		ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
		defer cancel()
		if record, found, err := repos.Character.Load(ctx, strconv.Itoa(int(charID))); err == nil && found && record.Name != "" {
			return record.Name
		}
	}
	return csharpSelectCharacterName(charID)
}

func (s *Service) sendRaidCommandFailure(session *gameSession, typ uint16) error {
	return s.sendGameUpperRawClassCodec(session, typ, []byte{0, runtimeRaidFailureCode}, dnfproto.DefaultChannelClassification, true)
}

func (s *Service) ensureRuntimeRaidMapsLocked() {
	if s.raids == nil {
		s.raids = make(map[uint32]*runtimeRaidState)
	}
}

func (s *Service) nextRuntimeRaidKeyLocked(session *gameSession, sourceID uint16) uint32 {
	channelID := uint32(0)
	if session != nil {
		channelID = uint32(uint16(session.channel.ID))
	}
	base := channelID<<16 | uint32(sourceID)
	if base != 0 {
		if _, exists := s.raids[base]; !exists {
			return base
		}
	}
	for {
		s.nextRaidSeq++
		key := channelID<<16 | uint32(uint16(s.nextRaidSeq))
		if key == 0 {
			continue
		}
		if _, exists := s.raids[key]; !exists {
			return key
		}
	}
}

func (s *Service) runtimeRaidByMemberLocked(charID uint16) *runtimeRaidState {
	if charID == 0 {
		return nil
	}
	for _, state := range s.raids {
		if runtimeRaidMemberIndex(*state, charID) >= 0 {
			return state
		}
	}
	return nil
}

func (s *Service) removeRuntimeRaidMemberLocked(charID uint16) (*runtimeRaidState, bool, bool) {
	state := s.runtimeRaidByMemberLocked(charID)
	if state == nil {
		return nil, false, false
	}
	next := state.Members[:0]
	removed := false
	for _, member := range state.Members {
		if member.CharID == charID {
			removed = true
			continue
		}
		next = append(next, member)
	}
	if !removed {
		return state, false, false
	}
	state.Members = next
	if len(state.Members) == 0 {
		delete(s.raids, state.RaidKey)
		return nil, true, true
	}
	if state.LeaderID == charID || runtimeRaidMemberIndex(*state, state.LeaderID) < 0 {
		state.LeaderID = state.Members[0].CharID
	}
	return state, true, false
}

func runtimeRaidMemberRefresh(state runtimeRaidState) raid.MemberRefresh {
	members := append([]runtimeRaidMemberState(nil), state.Members...)
	runtimeRaidSortMembers(members)
	out := raid.MemberRefresh{RaidKey: state.RaidKey, Members: make([]raid.MemberRecord, 0, len(members))}
	for _, member := range members {
		out.Members = append(out.Members, raid.MemberRecord{
			CharKey:           member.CharID,
			Field4:            member.UserState,
			Name:              member.Name,
			Field40:           0,
			Field44:           0,
			GroupIndex:        member.GroupIndex,
			SlotOrder:         member.SlotOrder,
			Field48:           0,
			Field52:           0,
			Field53:           0,
			Field56:           0,
			Field60:           0,
			Field64:           0,
			Field66BoolSource: boolUint32(member.CharID == state.LeaderID),
		})
	}
	return out
}

func runtimeRaidPartyGroups(state runtimeRaidState) [][]runtimeRaidMemberState {
	members := append([]runtimeRaidMemberState(nil), state.Members...)
	runtimeRaidSortMembers(members)
	groups := make([][]runtimeRaidMemberState, 0, runtimeRaidMaxGroups)
	byGroup := make(map[byte][]runtimeRaidMemberState)
	for _, member := range members {
		group := member.GroupIndex
		if group == 0 {
			group = runtimeRaidDefaultGroup
		}
		member.GroupIndex = group
		byGroup[group] = append(byGroup[group], member)
	}
	keys := make([]int, 0, len(byGroup))
	for group := range byGroup {
		keys = append(keys, int(group))
	}
	sort.Ints(keys)
	for _, key := range keys {
		groups = append(groups, byGroup[byte(key)])
	}
	return groups
}

func runtimeRaidHasOversizedGroup(state runtimeRaidState) bool {
	counts := make(map[byte]int)
	for _, member := range state.Members {
		group := member.GroupIndex
		if group == 0 {
			group = runtimeRaidDefaultGroup
		}
		counts[group]++
		if counts[group] > runtimeRaidGroupSize {
			return true
		}
	}
	return false
}

func runtimeRaidGroupPartyState(state runtimeRaidState, members []runtimeRaidMemberState) alignedcmd.PartyState {
	leaderID := members[0].CharID
	partyState := alignedcmd.PartyState{
		PartyID:    int(state.RaidKey&0xffff) + int(members[0].GroupIndex)*1000,
		IsLeader:   true,
		UserID:     leaderID,
		UserState:  1,
		NameBytes:  append([]byte(nil), state.NameBytes...),
		MaxMembers: runtimeRaidGroupSize,
		Members:    make([]alignedcmd.PartyMemberState, 0, len(members)),
	}
	if len(partyState.NameBytes) == 0 {
		partyState.NameBytes = []byte("raid")
	}
	for _, member := range members {
		partyState.Members = append(partyState.Members, alignedcmd.PartyMemberState{
			UserID:    member.CharID,
			UserState: firstByte(member.UserState, 1),
			HPPercent: firstByte(member.HPPercent, 100),
			MPPercent: firstByte(member.MPPercent, 100),
		})
	}
	return partyState
}

func runtimeRaidMemberIndex(state runtimeRaidState, charID uint16) int {
	for i, member := range state.Members {
		if member.CharID == charID {
			return i
		}
	}
	return -1
}

func nextRuntimeRaidSlot(state runtimeRaidState, group byte) byte {
	return nextRuntimeRaidSlotExcluding(state, group, 0)
}

func nextRuntimeRaidSlotExcluding(state runtimeRaidState, group byte, exclude uint16) byte {
	used := make(map[byte]bool)
	for _, member := range state.Members {
		if member.CharID == exclude || member.GroupIndex != group {
			continue
		}
		used[member.SlotOrder] = true
	}
	for i := byte(0); i < runtimeRaidGroupSize; i++ {
		if !used[i] {
			return i
		}
	}
	return runtimeRaidGroupSize - 1
}

func runtimeRaidSortMembers(members []runtimeRaidMemberState) {
	sort.SliceStable(members, func(i, j int) bool {
		leftGroup := raidSortGroup(members[i].GroupIndex)
		rightGroup := raidSortGroup(members[j].GroupIndex)
		if leftGroup != rightGroup {
			return leftGroup < rightGroup
		}
		if members[i].SlotOrder != members[j].SlotOrder {
			return members[i].SlotOrder < members[j].SlotOrder
		}
		return members[i].CharID < members[j].CharID
	})
}

func raidSortGroup(group byte) int {
	if group == 0 {
		return 1000
	}
	return int(group)
}

func cloneRuntimeRaidState(state runtimeRaidState) runtimeRaidState {
	state.NameBytes = append([]byte(nil), state.NameBytes...)
	state.Members = append([]runtimeRaidMemberState(nil), state.Members...)
	return state
}

func boolUint32(value bool) uint32 {
	if value {
		return 1
	}
	return 0
}

func firstByte(value byte, fallback byte) byte {
	if value != 0 {
		return value
	}
	return fallback
}
