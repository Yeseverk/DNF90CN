package dnfbridge

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path"
	"strconv"
	"strings"
	"time"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfdungeon "longheng.io/server/internal/modules/dnf/dungeon"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

const (
	currentDungeonTutorialRewardProgress = uint32(38)

	currentDungeonTutorialRewardPVFPath = "etc/serverparameter.etc"
	currentDungeonTutorialRewardSection = "escalade tutorial reward"
	currentDungeonTutorialStackableList = "stackable/stackable.lst"

	// Current main-list slots 3..8 are the six item quick slots. Slot 3 is
	// the first visible quick slot; ordinary consumables fall back to 65..120.
	currentDungeonTutorialQuickSlotStart = int16(3)
	currentDungeonTutorialQuickSlotEnd   = int16(8)

	currentDungeonTutorialRewardMarkerValue = int64(1)
)

var (
	errDungeonTutorialRewardSourceRequired = errors.New("dnf tutorial reward PVF source is required")
	errDungeonTutorialRewardSectionMissing = errors.New("dnf tutorial reward PVF section is missing")
	errDungeonTutorialRewardRowInvalid     = errors.New("dnf tutorial reward PVF row is invalid")
	errDungeonTutorialRewardMissing        = errors.New("dnf tutorial reward progress has no PVF row")
	errDungeonTutorialRewardItemInvalid    = errors.New("dnf tutorial reward item is not a supported consumable")
	errDungeonTutorialRewardAssetMissing   = errors.New("dnf tutorial reward asset transaction is unavailable")
	errDungeonTutorialRewardInventoryFull  = errors.New("dnf tutorial reward inventory is full")
	errDungeonTutorialRewardResponse       = errors.New("dnf tutorial reward response is invalid")
)

type pvfDungeonTutorialReward struct {
	Progress uint32
	ItemID   uint32
	Count    uint32
}

type pvfDungeonTutorialRewardCatalog struct {
	byProgress map[uint32][]pvfDungeonTutorialReward
}

type currentDungeonTutorialRewardItemDefinition struct {
	ItemID         uint32
	PVFPath        string
	StackableType  string
	StackLimit     int64
	SlotStart      int16
	SlotEnd        int16
	ExpirationDate time.Time
}

func newPVFDungeonTutorialRewardCatalog(source dnfpvf.Source) (*pvfDungeonTutorialRewardCatalog, error) {
	if source == nil {
		return nil, errDungeonTutorialRewardSourceRequired
	}
	text, err := source.ReadText(currentDungeonTutorialRewardPVFPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", currentDungeonTutorialRewardPVFPath, err)
	}
	document, err := dnfpvf.Parse(currentDungeonTutorialRewardPVFPath, text)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", currentDungeonTutorialRewardPVFPath, err)
	}
	tokens, ok := document.Section(currentDungeonTutorialRewardSection)
	if !ok {
		return nil, errDungeonTutorialRewardSectionMissing
	}
	values := make([]int64, 0, len(tokens))
	for _, token := range tokens {
		if token.Kind != dnfpvf.TokenInt {
			return nil, fmt.Errorf(
				"%w: line=%d kind=%s raw=%q",
				errDungeonTutorialRewardRowInvalid,
				token.Line,
				token.Kind,
				token.Raw,
			)
		}
		values = append(values, token.Int)
	}
	if len(values) == 0 || len(values)%3 != 0 {
		return nil, fmt.Errorf("%w: value_count=%d want_positive_triples", errDungeonTutorialRewardRowInvalid, len(values))
	}

	byProgress := make(map[uint32][]pvfDungeonTutorialReward)
	for index := 0; index < len(values); index += 3 {
		progress, itemID, count := values[index], values[index+1], values[index+2]
		if progress <= 0 || progress > math.MaxUint32 ||
			itemID <= 0 || itemID > math.MaxUint32 ||
			count <= 0 || count > math.MaxUint32 {
			return nil, fmt.Errorf(
				"%w: row=%d progress=%d item=%d count=%d",
				errDungeonTutorialRewardRowInvalid,
				index/3,
				progress,
				itemID,
				count,
			)
		}
		key := uint32(progress)
		if len(byProgress[key]) >= math.MaxUint8 {
			return nil, fmt.Errorf("%w: progress=%d reward_count_exceeds_u8", errDungeonTutorialRewardRowInvalid, key)
		}
		byProgress[key] = append(byProgress[key], pvfDungeonTutorialReward{
			Progress: key,
			ItemID:   uint32(itemID),
			Count:    uint32(count),
		})
	}
	return &pvfDungeonTutorialRewardCatalog{byProgress: byProgress}, nil
}

func (c *pvfDungeonTutorialRewardCatalog) Rewards(progress uint32) []pvfDungeonTutorialReward {
	if c == nil {
		return nil
	}
	return append([]pvfDungeonTutorialReward(nil), c.byProgress[progress]...)
}

type currentDungeonTutorialRewardDefinition struct {
	Reward pvfDungeonTutorialReward
	Item   currentDungeonTutorialRewardItemDefinition
}

type currentDungeonTutorialRewardRow struct {
	Slot   uint16
	ItemID uint32
	Count  uint32
}

type currentDungeonTutorialRewardDelivery struct {
	Granted bool
	Rows    []currentDungeonTutorialRewardRow
}

func (s *Service) handleCurrentDungeonTutorialReward(session *gameSession, progress uint32) error {
	if session == nil || progress != currentDungeonTutorialRewardProgress {
		return nil
	}
	selectedCharacterID := session.selectedCharacterID
	if selectedCharacterID == 0 {
		s.logGameEvent(session, "game-dungeon-tutorial-reward-blocked",
			"progress", progress,
			"reason", "selected_character_missing")
		return nil
	}

	session.dungeon.mu.Lock()
	defer session.dungeon.mu.Unlock()
	runtime := session.dungeon.runtime
	if runtime == nil || runtime.Session == nil || runtime.Room == nil {
		s.logGameEvent(session, "game-dungeon-tutorial-reward-blocked",
			"char_id", selectedCharacterID,
			"progress", progress,
			"reason", "active_dungeon_runtime_missing")
		return nil
	}
	if !dungeonRuntimeOwnsCharacter(runtime, selectedCharacterID) {
		s.logGameEvent(session, "game-dungeon-tutorial-reward-blocked",
			"char_id", selectedCharacterID,
			"runtime_char_id", runtime.Character.CharacterID,
			"progress", progress,
			"reason", "active_dungeon_runtime_character_owner_mismatch")
		return nil
	}
	snapshot := runtime.Session.Snapshot()
	if snapshot.Run.Status != worldmap.DungeonRunActive || !isPVFTutorialDungeonScene(runtime, snapshot.Scene) {
		s.logGameEvent(session, "game-dungeon-tutorial-reward-blocked",
			"char_id", selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"map_id", snapshot.Scene.Map.Map.ID,
			"run_status", snapshot.Run.Status,
			"progress", progress,
			"reason", "owned_active_pvf_tutorial_scene_missing")
		return nil
	}

	definitions, err := s.resolveCurrentDungeonTutorialRewards(progress)
	if err != nil {
		s.logGameEvent(session, "game-dungeon-tutorial-reward-blocked",
			"char_id", selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"map_id", snapshot.Scene.Map.Map.ID,
			"progress", progress,
			"reason", "runtime_pvf_tutorial_reward_unavailable",
			"error", err)
		return nil
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.CharacterAssets == nil {
		s.logGameEvent(session, "game-dungeon-tutorial-reward-blocked",
			"char_id", selectedCharacterID,
			"progress", progress,
			"reason", "character_asset_transaction_missing",
			"error", errDungeonTutorialRewardAssetMissing)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), createWriteTimeout)
	defer cancel()
	delivery, err := deliverCurrentDungeonTutorialReward(
		ctx,
		repositories.CharacterAssets,
		strconv.Itoa(int(selectedCharacterID)),
		progress,
		definitions,
		time.Now().UTC(),
	)
	if err != nil {
		s.logGameEvent(session, "game-dungeon-tutorial-reward-blocked",
			"char_id", selectedCharacterID,
			"dungeon_id", runtime.Dungeon.ID,
			"map_id", snapshot.Scene.Map.Map.ID,
			"progress", progress,
			"reason", "atomic_character_reward_transaction_failed",
			"error", err)
		return nil
	}
	payload, err := buildCurrentDungeonTutorialRewardSuccessPayload(delivery.Rows)
	if err != nil {
		s.logGameEvent(session, "game-dungeon-tutorial-reward-blocked-after-commit",
			"char_id", selectedCharacterID,
			"progress", progress,
			"granted", delivery.Granted,
			"reason", "current_exe_op143_reward_response_invalid",
			"error", err)
		return nil
	}
	itemIDs := make([]uint32, 0, len(delivery.Rows))
	slots := make([]uint16, 0, len(delivery.Rows))
	counts := make([]uint32, 0, len(delivery.Rows))
	for _, row := range delivery.Rows {
		itemIDs = append(itemIDs, row.ItemID)
		slots = append(slots, row.Slot)
		counts = append(counts, row.Count)
	}
	s.logGameEvent(session, "game-dungeon-tutorial-reward-op143-ack-send",
		"char_id", selectedCharacterID,
		"dungeon_id", runtime.Dungeon.ID,
		"room", snapshot.Run.Current.String(),
		"map_id", snapshot.Scene.Map.Map.ID,
		"progress", progress,
		"msg_id", uint16(dnfenum.CmdPacketChangeTutorialFlag),
		"classification", 1,
		"success", 1,
		"reward_count", len(delivery.Rows),
		"item_ids", itemIDs,
		"slots", slots,
		"counts", counts,
		"new_grant", delivery.Granted,
		"body_len", len(payload)+1,
		"body_source", "current_exe_sub_33C4A20_u8_count_then_u16_slot_u32_item_u32_count",
		"asset_owner", "character_assets_uow",
		"reward_source", currentDungeonTutorialRewardPVFPath+"_["+currentDungeonTutorialRewardSection+"]")
	return s.sendGameUpperSuccess(
		session,
		uint16(dnfenum.CmdPacketChangeTutorialFlag),
		payload,
	)
}

func (s *Service) resolveCurrentDungeonTutorialRewards(progress uint32) ([]currentDungeonTutorialRewardDefinition, error) {
	monsterCatalog, err := s.dungeonMonsterCatalog()
	if err != nil {
		return nil, err
	}
	rewardCatalog, err := newPVFDungeonTutorialRewardCatalog(monsterCatalog.source)
	if err != nil {
		return nil, err
	}
	rewards := rewardCatalog.Rewards(progress)
	if len(rewards) == 0 {
		return nil, fmt.Errorf("%w: progress=%d", errDungeonTutorialRewardMissing, progress)
	}
	definitions := make([]currentDungeonTutorialRewardDefinition, 0, len(rewards))
	for _, reward := range rewards {
		definition, err := resolveCurrentDungeonTutorialRewardItem(monsterCatalog.source, reward.ItemID)
		if err != nil {
			return nil, err
		}
		if !isCurrentDungeonTutorialConsumable(definition.StackableType) {
			return nil, fmt.Errorf(
				"%w: progress=%d item=%d stackable_type=%q",
				errDungeonTutorialRewardItemInvalid,
				progress,
				reward.ItemID,
				definition.StackableType,
			)
		}
		if definition.StackLimit > 0 && uint64(reward.Count) > uint64(definition.StackLimit) {
			return nil, fmt.Errorf(
				"%w: progress=%d item=%d count=%d stack_limit=%d",
				errDungeonTutorialRewardItemInvalid,
				progress,
				reward.ItemID,
				reward.Count,
				definition.StackLimit,
			)
		}
		definitions = append(definitions, currentDungeonTutorialRewardDefinition{Reward: reward, Item: definition})
	}
	return definitions, nil
}

func resolveCurrentDungeonTutorialRewardItem(
	source dnfpvf.Source,
	itemID uint32,
) (currentDungeonTutorialRewardItemDefinition, error) {
	if source == nil || itemID == 0 {
		return currentDungeonTutorialRewardItemDefinition{}, errDungeonTutorialRewardSourceRequired
	}
	text, err := source.ReadText(currentDungeonTutorialStackableList)
	if err != nil {
		return currentDungeonTutorialRewardItemDefinition{}, fmt.Errorf("read %s: %w", currentDungeonTutorialStackableList, err)
	}
	document, err := dnfpvf.Parse(currentDungeonTutorialStackableList, text)
	if err != nil {
		return currentDungeonTutorialRewardItemDefinition{}, fmt.Errorf("parse %s: %w", currentDungeonTutorialStackableList, err)
	}
	listedPath := ""
	for _, entry := range dnfpvf.ParseList(document) {
		if entry.ID == int64(itemID) {
			listedPath = cleanCurrentDungeonTutorialRewardPath(entry.Path)
			break
		}
	}
	if listedPath == "" {
		return currentDungeonTutorialRewardItemDefinition{}, fmt.Errorf(
			"%w: item=%d absent_from=%s",
			errDungeonTutorialRewardItemInvalid,
			itemID,
			currentDungeonTutorialStackableList,
		)
	}
	candidates := []string{listedPath}
	if !strings.HasPrefix(strings.ToLower(listedPath), "stackable/") {
		candidates = append(candidates, path.Join("stackable", listedPath))
	}
	var itemPath string
	var itemText string
	var lastErr error
	for _, candidate := range candidates {
		itemText, lastErr = source.ReadText(candidate)
		if lastErr == nil {
			itemPath = candidate
			break
		}
	}
	if itemPath == "" {
		return currentDungeonTutorialRewardItemDefinition{}, fmt.Errorf(
			"read tutorial reward item=%d path=%s: %w",
			itemID,
			listedPath,
			lastErr,
		)
	}
	itemDocument, err := dnfpvf.Parse(itemPath, itemText)
	if err != nil {
		return currentDungeonTutorialRewardItemDefinition{}, fmt.Errorf(
			"parse tutorial reward item=%d path=%s: %w",
			itemID,
			itemPath,
			err,
		)
	}
	stackableType, _ := itemDocument.Text("stackable type")
	definition := currentDungeonTutorialRewardItemDefinition{
		ItemID:        itemID,
		PVFPath:       itemPath,
		StackableType: stackableType,
		SlotStart:     65,
		SlotEnd:       120,
	}
	if limit, found := itemDocument.Int("stack limit"); found && limit > 0 {
		definition.StackLimit = limit
	}
	expirationDate, err := parseCurrentPVFExpirationDate(itemDocument)
	if err != nil {
		return currentDungeonTutorialRewardItemDefinition{}, fmt.Errorf("parse tutorial reward item=%d path=%s expiration date: %w", itemID, itemPath, err)
	}
	definition.ExpirationDate = expirationDate
	return definition, nil
}

func cleanCurrentDungeonTutorialRewardPath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	value = strings.TrimPrefix(value, "./")
	return strings.TrimPrefix(value, "/")
}

func isCurrentDungeonTutorialConsumable(stackableType string) bool {
	normalized := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(stackableType, "`", "")))
	return strings.HasPrefix(normalized, "[waste]")
}

func deliverCurrentDungeonTutorialReward(
	ctx context.Context,
	uow dnfrepo.CharacterAssetUnitOfWork,
	characterID string,
	progress uint32,
	definitions []currentDungeonTutorialRewardDefinition,
	now time.Time,
) (currentDungeonTutorialRewardDelivery, error) {
	if uow == nil || strings.TrimSpace(characterID) == "" || progress == 0 || len(definitions) == 0 {
		return currentDungeonTutorialRewardDelivery{}, errDungeonTutorialRewardAssetMissing
	}
	owner, err := dnfdungeon.NewOwner(dnfrepo.Group{CharacterAssets: uow})
	if err != nil {
		return currentDungeonTutorialRewardDelivery{}, errDungeonTutorialRewardAssetMissing
	}
	rewards := make([]dnfdungeon.TutorialItemReward, 0, len(definitions))
	for _, definition := range definitions {
		rewards = append(rewards, currentDungeonTutorialDomainReward(definition))
	}
	result, err := owner.GrantTutorialReward(ctx, dnfdungeon.TutorialRewardCommand{
		CharacterID: characterID,
		Progress:    progress,
		Rewards:     rewards,
		UpdatedAt:   now,
		Project: func(stack dnfrepo.ItemStack, expiration time.Time) (dnfrepo.ItemStack, error) {
			stack, _ = applyCurrentStackableExpirationAt(stack, expiration, now)
			return stack, nil
		},
	})
	if err != nil {
		return currentDungeonTutorialRewardDelivery{}, mapCurrentDungeonTutorialOwnerError(err)
	}
	delivery := currentDungeonTutorialRewardDelivery{Granted: result.Granted}
	for _, row := range result.Rows {
		delivery.Rows = append(delivery.Rows, currentDungeonTutorialRewardRow{
			Slot: row.Slot, ItemID: row.ItemID, Count: row.Count,
		})
	}
	return delivery, nil
}

func currentDungeonTutorialRewardMarker(progress uint32) string {
	return dnfdungeon.TutorialRewardMarker(progress)
}

func addCurrentDungeonTutorialRewardToInventory(
	record *dnfrepo.InventoryRecord,
	progress uint32,
	definition currentDungeonTutorialRewardDefinition,
) (int16, error) {
	reward := currentDungeonTutorialDomainReward(definition)
	slot, err := dnfdungeon.PlaceTutorialReward(
		record,
		progress,
		reward,
		func(stack dnfrepo.ItemStack, expiration time.Time) (dnfrepo.ItemStack, error) {
			stack, _ = applyCurrentStackableExpiration(stack, expiration)
			return stack, nil
		},
	)
	if err != nil {
		return 0, mapCurrentDungeonTutorialOwnerError(err)
	}
	return int16(slot), nil
}

func currentDungeonTutorialDomainReward(
	definition currentDungeonTutorialRewardDefinition,
) dnfdungeon.TutorialItemReward {
	return dnfdungeon.TutorialItemReward{
		Progress: definition.Reward.Progress,
		ItemID:   definition.Reward.ItemID,
		Count:    definition.Reward.Count,
		Consumable: definition.Item.ItemID == definition.Reward.ItemID &&
			isCurrentDungeonTutorialConsumable(definition.Item.StackableType),
		StackLimit:    definition.Item.StackLimit,
		SlotStart:     definition.Item.SlotStart,
		SlotEnd:       definition.Item.SlotEnd,
		ExpireAt:      definition.Item.ExpirationDate,
		PVFPath:       definition.Item.PVFPath,
		StackableType: definition.Item.StackableType,
	}
}

func mapCurrentDungeonTutorialOwnerError(err error) error {
	switch {
	case errors.Is(err, dnfdungeon.ErrOwnerUnavailable),
		errors.Is(err, dnfdungeon.ErrTutorialAssetsMissing):
		return errDungeonTutorialRewardAssetMissing
	case errors.Is(err, dnfdungeon.ErrTutorialInventoryFull):
		return fmt.Errorf("%w: %v", errDungeonTutorialRewardInventoryFull, err)
	case errors.Is(err, dnfdungeon.ErrTutorialRewardInvalid),
		errors.Is(err, dnfdungeon.ErrStackProjectorRequired):
		return fmt.Errorf("%w: %v", errDungeonTutorialRewardItemInvalid, err)
	default:
		return err
	}
}

func currentDungeonTutorialMainSlotKey(slot int16) string {
	return "0:" + strconv.FormatInt(int64(slot), 10)
}

func buildCurrentDungeonTutorialRewardSuccessPayload(rows []currentDungeonTutorialRewardRow) ([]byte, error) {
	if len(rows) > math.MaxUint8 {
		return nil, fmt.Errorf("%w: reward_count=%d", errDungeonTutorialRewardResponse, len(rows))
	}
	var writer packetWriter
	writer.writeByte(byte(len(rows)))
	for index, row := range rows {
		if row.Slot == 0 || row.ItemID == 0 || row.Count == 0 {
			return nil, fmt.Errorf(
				"%w: row=%d slot=%d item=%d count=%d",
				errDungeonTutorialRewardResponse,
				index,
				row.Slot,
				row.ItemID,
				row.Count,
			)
		}
		writer.writeUint16(row.Slot)
		writer.writeUint32(row.ItemID)
		writer.writeUint32(row.Count)
	}
	return writer.bytes(), nil
}
