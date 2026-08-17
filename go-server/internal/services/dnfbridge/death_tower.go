package dnfbridge

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfquest "longheng.io/server/internal/modules/dnf/quest"
)

var (
	errDeathTowerUnavailable = errors.New("dnf death tower is unavailable")
	errDeathTowerNotInTower  = errors.New("dnf death tower session is not active")
	errDeathTowerAdvance     = errors.New("dnf death tower stage advance rejected")
)

const (
	currentDeathTowerDungeonID     = 11000
	currentDeathTowerTotalStages   = 45
	currentDeathTowerFirstMapID    = 30001
	currentDeathTowerBasisLevel    = 60
	currentDeathTowerInfoMsgID     = 0x008E
	currentDeathTowerStageMapMsgID = 0x008F
	currentDeathTowerRankingMsgID  = 0x0090
	currentDeathTowerRewardMsgID   = 0x0091
	currentDeathTowerEplpMsgID     = 0x0092
	currentDeathTowerNormalMode    = 0
	currentDeathTowerBuffType      = 11
)

// deathTowerRuntime is the per-session tower state.
type deathTowerRuntime struct {
	DungeonID    int
	CurrentStage int // 0-based
	TotalStages  int
	State        int // 0=init, 1=fighting, 2=cleared
	MonsterSeq   uint16
	StageMapIDs  []int
	BasisLevel   int
}

func newDeathTowerRuntime(stageMapIDs []int, basisLevel int) *deathTowerRuntime {
	return &deathTowerRuntime{
		DungeonID:    currentDeathTowerDungeonID,
		CurrentStage: 0,
		TotalStages:  len(stageMapIDs),
		State:        0,
		MonsterSeq:   1,
		StageMapIDs:  stageMapIDs,
		BasisLevel:   basisLevel,
	}
}

func (rt *deathTowerRuntime) currentMapID() int {
	if rt == nil || rt.CurrentStage < 0 || rt.CurrentStage >= len(rt.StageMapIDs) {
		return 0
	}
	return rt.StageMapIDs[rt.CurrentStage]
}

func (rt *deathTowerRuntime) isLastStage() bool {
	return rt != nil && rt.CurrentStage >= rt.TotalStages-1
}

func (rt *deathTowerRuntime) tryAdvance() bool {
	if rt == nil || rt.State < 1 || rt.CurrentStage >= rt.TotalStages-1 {
		return false
	}
	rt.CurrentStage++
	rt.State = 0
	return true
}

// --- Entry ---

func (s *Service) handleCurrentDeathTowerEntry(session *gameSession, difficulty byte) error {
	if session == nil || session.selectedCharacterID == 0 {
		return errDeathTowerUnavailable
	}
	stageMapIDs, basisLevel, err := s.currentDeathTowerPVFConfig()
	if err != nil {
		s.logGameEvent(session, "game-death-tower-entry-rejected", "reason", err)
		return s.sendGameUpperFailure(session, uint16(dnfenum.CmdPacketSelectDungeon), 22)
	}
	rt := newDeathTowerRuntime(stageMapIDs, basisLevel)
	session.dungeon.deathTower = rt

	s.logGameEvent(session, "game-death-tower-entry",
		"dungeon_id", rt.DungeonID,
		"total_stages", rt.TotalStages,
		"basis_level", rt.BasisLevel,
		"first_map", rt.currentMapID())

	// 0x008E DEATH_TOWER_INFO (8B)
	infoBody := buildCurrentDeathTowerInfo(rt.DungeonID, uint16(rt.TotalStages))
	if err := s.sendGameUpperRawClass(session, currentDeathTowerInfoMsgID, infoBody, dnfproto.DefaultChannelClassification); err != nil {
		return err
	}
	// 0x008F first stage map
	if err := s.sendCurrentDeathTowerStageMap(session, rt); err != nil {
		return err
	}
	// 0x001E FINISH_LOADING (empty)
	return s.sendGameUpperRawClass(session, csharpNotiFinishLoadingMsgID, nil, dnfproto.DefaultChannelClassification)
}

// --- Stage Map ---

func (s *Service) sendCurrentDeathTowerStageMap(session *gameSession, rt *deathTowerRuntime) error {
	monsters, err := s.currentDeathTowerStageMonsters(rt)
	if err != nil {
		s.logGameEvent(session, "game-death-tower-stage-map-failed",
			"stage", rt.CurrentStage, "map_id", rt.currentMapID(), "reason", err)
		monsters = nil
	}
	body := buildCurrentDeathTowerStageMap(rt, monsters)
	return s.sendGameUpperRawClass(session, currentDeathTowerStageMapMsgID, body, dnfproto.DefaultChannelClassification)
}

// --- Stage Command (op159 / CmdPacketDeathTowerStageCmd) ---

func (s *Service) handleCurrentDeathTowerStageCmd(session *gameSession, body []byte) error {
	rt := session.dungeon.deathTower
	if rt == nil {
		return nil
	}
	if len(body) < 1 {
		return nil
	}
	switch body[0] {
	case 1:
		rt.State = 1
		s.logGameEvent(session, "game-death-tower-fight-start", "stage", rt.CurrentStage)
	case 2:
		rt.State = 2
		s.logGameEvent(session, "game-death-tower-stage-clear", "stage", rt.CurrentStage, "map_id", rt.currentMapID(), "is_last", rt.isLastStage())
		s.syncCurrentDeathTowerClearMap(session, rt, "tower_stage_cmd")
		if rt.isLastStage() {
			return s.sendCurrentDeathTowerSettlement(session, rt)
		}
	}
	return nil
}

// --- MOVE_MAP advance ---

func (s *Service) handleCurrentDeathTowerMoveMap(session *gameSession) (bool, error) {
	rt := session.dungeon.deathTower
	if rt == nil {
		return false, nil
	}
	if rt.State >= 1 {
		s.syncCurrentDeathTowerClearMap(session, rt, "tower_move_map")
	}
	if !rt.tryAdvance() {
		s.logGameEvent(session, "game-death-tower-advance-rejected",
			"stage", rt.CurrentStage, "state", rt.State, "is_last", rt.isLastStage())
		return true, nil
	}
	s.logGameEvent(session, "game-death-tower-advance",
		"stage", rt.CurrentStage, "map_id", rt.currentMapID())
	if err := s.sendCurrentDeathTowerStageMap(session, rt); err != nil {
		return true, err
	}
	return true, s.sendGameUpperRawClass(session, csharpNotiFinishLoadingMsgID, nil, dnfproto.DefaultChannelClassification)
}

// --- Settlement ---

func (s *Service) sendCurrentDeathTowerSettlement(session *gameSession, rt *deathTowerRuntime) error {
	s.logGameEvent(session, "game-death-tower-settlement", "dungeon_id", rt.DungeonID, "total_stages", rt.TotalStages)
	// 0x0090 ranking (empty)
	if err := s.sendGameUpperRawClass(session, currentDeathTowerRankingMsgID, buildCurrentDeathTowerEmptyRanking(rt.DungeonID), dnfproto.DefaultChannelClassification); err != nil {
		return err
	}
	// 0x0091 reward (empty)
	if err := s.sendGameUpperRawClass(session, currentDeathTowerRewardMsgID, buildCurrentDeathTowerEmptyReward(), dnfproto.DefaultChannelClassification); err != nil {
		return err
	}
	// 0x0092 eplp
	if err := s.sendGameUpperRawClass(session, currentDeathTowerEplpMsgID, []byte{1}, dnfproto.DefaultChannelClassification); err != nil {
		return err
	}
	session.dungeon.deathTower = nil
	return nil
}

// --- Quest sync ---

func (s *Service) syncCurrentDeathTowerClearMap(session *gameSession, rt *deathTowerRuntime, source string) {
	mapID := rt.currentMapID()
	if mapID <= 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	characterID := strconv.FormatUint(uint64(session.selectedCharacterID), 10)
	repos, ok := s.repositoryGroup()
	if !ok || repos.Quest == nil || repos.Character == nil {
		return
	}
	if _, found, err := repos.Quest.Load(ctx, characterID); err != nil || !found {
		return
	}
	catalog, err := s.loadQuestCatalog(ctx)
	if err != nil {
		return
	}
	owner, err := dnfquest.NewOwner(repos)
	if err != nil {
		return
	}
	completionKey := fmt.Sprintf("death-tower/%s/%d/%d", characterID, rt.CurrentStage, mapID)
	result, err := owner.ApplyClearMapCompletion(ctx, catalog, characterID, dnfquest.ClearMapCompletionInput{
		DungeonID:     int64(rt.DungeonID),
		MapID:         int64(mapID),
		CompletionKey: completionKey,
		CompletedAt:   time.Now().UTC(),
	})
	if err != nil {
		s.logGameEvent(session, "game-death-tower-quest-sync-failed",
			"stage", rt.CurrentStage, "map_id", mapID, "source", source, "reason", err)
		return
	}
	if len(result.Completions) > 0 {
		s.logGameEvent(session, "game-death-tower-quest-synced",
			"stage", rt.CurrentStage, "map_id", mapID, "source", source, "completions", len(result.Completions))
	}
}

// --- Packet builders ---

func buildCurrentDeathTowerInfo(dungeonID int, endStage uint16) []byte {
	var w packetWriter
	w.writeUint32(uint32(dungeonID))
	w.writeUint16(endStage)
	w.writeByte(currentDeathTowerNormalMode)
	w.writeByte(currentDeathTowerBuffType)
	return w.bytes()
}

type currentDeathTowerStageMonster struct {
	ListIndex     uint32
	MonsterUnique uint16
	MonsterID     uint32
	Level         byte
	MonsterType   byte // 0=normal, 5=APC
	IsBox         byte
	BoxIndex      byte
}

func buildCurrentDeathTowerStageMap(rt *deathTowerRuntime, monsters []currentDeathTowerStageMonster) []byte {
	var w packetWriter
	w.writeUint16(uint16(rt.CurrentStage + 1)) // 1-based display
	w.writeUint32(rand.Uint32())
	w.writeUint16(uint16(rt.currentMapID()))
	w.writeByte(byte(len(monsters)))
	for _, m := range monsters {
		w.writeUint32(m.ListIndex)
		w.writeUint16(m.MonsterUnique)
		w.writeUint32(m.MonsterID)
		w.writeByte(m.Level)
		w.writeByte(m.MonsterType)
		w.writeByte(m.IsBox)
		w.writeByte(m.BoxIndex)
	}
	w.writeByte(0) // itemCount = 0
	return w.bytes()
}

func buildCurrentDeathTowerEmptyRanking(dungeonID int) []byte {
	var w packetWriter
	w.writeByte(0)   // flag0
	w.writeUint32(0) // clearTime
	w.writeUint32(0) // playTime
	w.writeByte(0)   // flag3
	w.writeUint32(uint32(dungeonID))
	w.writeByte(0) // hasMyBestRecord = false
	for g := 0; g < 5; g++ {
		for r := 0; r < 8; r++ {
			w.writeUint32(0) // empty dstr
			w.writeByte(0)
			w.writeByte(0)
		}
		w.writeUint16(0)
		w.writeUint32(0)
		w.writeUint32(0)
	}
	return w.bytes()
}

func buildCurrentDeathTowerEmptyReward() []byte {
	var w packetWriter
	w.writeUint32(0) // summaryValue
	w.writeByte(0)   // group0 count
	w.writeByte(0)   // group1 count
	w.writeByte(0)   // group2 count
	w.writeByte(0)   // group3 count
	return w.bytes()
}

// --- PVF config ---

func (s *Service) currentDeathTowerPVFConfig() ([]int, int, error) {
	s.deathTowerMu.Lock()
	defer s.deathTowerMu.Unlock()
	if s.deathTowerStageMapIDs != nil {
		return s.deathTowerStageMapIDs, s.deathTowerBasisLevel, nil
	}
	s.initialEquipmentMu.Lock()
	archive, err := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %v", errDeathTowerUnavailable, err)
	}
	text, err := archive.ReadText("dungeon/Towers/TowerOfDeath.dgn")
	if err != nil {
		return nil, 0, fmt.Errorf("%w: read dgn: %v", errDeathTowerUnavailable, err)
	}
	doc, err := dnfpvf.Parse("dungeon/Towers/TowerOfDeath.dgn", text)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: parse dgn: %v", errDeathTowerUnavailable, err)
	}
	mapIDs, err := parseDeathTowerMapIndexes(doc)
	if err != nil {
		return nil, 0, err
	}
	basisLevel := 1
	if lv, found := doc.Int("basis level"); found && lv > 0 {
		basisLevel = int(lv)
	}
	s.deathTowerStageMapIDs = mapIDs
	s.deathTowerBasisLevel = basisLevel
	s.logPacketEvent("dnf-death-tower-pvf-loaded", "stages", len(mapIDs), "basis_level", basisLevel, "first_map", mapIDs[0], "last_map", mapIDs[len(mapIDs)-1])
	return mapIDs, basisLevel, nil
}

func parseDeathTowerMapIndexes(doc *dnfpvf.Document) ([]int, error) {
	values := doc.Ints("death tower map indexes")
	if len(values) < 3 {
		return nil, fmt.Errorf("%w: death tower map indexes missing or too short (%d values)", errDeathTowerUnavailable, len(values))
	}
	totalStages := int(values[0])
	if totalStages <= 0 || len(values) < 1+totalStages*2 {
		return nil, fmt.Errorf("%w: death tower map indexes malformed: total=%d values=%d", errDeathTowerUnavailable, totalStages, len(values))
	}
	mapIDs := make([]int, totalStages)
	for i := 0; i < totalStages; i++ {
		mapIDs[i] = int(values[1+i*2+1])
	}
	return mapIDs, nil
}

// --- Stage monster loading ---

func (s *Service) currentDeathTowerStageMonsters(rt *deathTowerRuntime) ([]currentDeathTowerStageMonster, error) {
	mapID := rt.currentMapID()
	if mapID <= 0 {
		return nil, errDeathTowerNotInTower
	}
	s.initialEquipmentMu.Lock()
	archive, err := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if err != nil {
		return nil, err
	}
	mapPath := fmt.Sprintf("map/DeadTower/%03dF.map", rt.CurrentStage+1)
	text, err := archive.ReadText(mapPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", mapPath, err)
	}
	doc, err := dnfpvf.Parse(mapPath, text)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", mapPath, err)
	}
	return parseDeathTowerMapMonsters(doc, rt)
}

// parseDeathTowerMapMonsters reads the [monster] section from a tower map file.
// Format per monster: monsterId groupIndex level x y direction countPerAI unk aiType rank
// (9 tokens per entry based on PVF observation).
func parseDeathTowerMapMonsters(doc *dnfpvf.Document, rt *deathTowerRuntime) ([]currentDeathTowerStageMonster, error) {
	values := doc.Ints("monster")
	if len(values) == 0 {
		return nil, nil
	}
	const stride = 9
	var monsters []currentDeathTowerStageMonster
	for offset := 0; offset+stride <= len(values); offset += stride {
		monsterID := values[offset]
		level := values[offset+2]
		if monsterID <= 0 {
			continue
		}
		if level <= 0 {
			level = int64(rt.BasisLevel)
		}
		if level > 255 {
			level = 255
		}
		monsters = append(monsters, currentDeathTowerStageMonster{
			ListIndex:     uint32(len(monsters)),
			MonsterUnique: rt.MonsterSeq,
			MonsterID:     uint32(monsterID),
			Level:         byte(level),
			MonsterType:   0,
			IsBox:         0,
			BoxIndex:      0,
		})
		rt.MonsterSeq++
	}
	return monsters, nil
}
