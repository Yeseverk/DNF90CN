package packageitem

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"time"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var (
	ErrOwnerUnavailable             = errors.New("package owner unavailable")
	ErrCharacterRequired            = errors.New("selected character id required")
	ErrInventoryNotFound            = errors.New("inventory record not found")
	ErrSlotNotFound                 = errors.New("package source slot not found")
	ErrMaterialNotFound             = errors.New("package material slot not found")
	ErrRewardMissing                = errors.New("package reward evidence is missing")
	ErrMagicBoxResolverRequired     = errors.New("magic box requires runtime-PVF resolvers")
	ErrInventoryTransactionRequired = errors.New("package mutation requires a character item transaction")
	ErrMailboxTransactionRequired   = errors.New("package overflow requires a mailbox asset transaction")
	ErrRewardInventoryFull          = errors.New("package reward target inventory is full")
)

// MagicBoxResult 描述一次随机盒开启的写库结果；Success=false 仍映射为
// 同 opcode 的 {0x00} 失败回包（该家族没有细分错误码），Changed 只表示
// repository 状态已变化。OpenCount 是实际开启次数：单开恒为 1，连开按
// 请求数、盒子数和材料数的最小值收敛。DoubleRewards 是赛丽亚幸运值满
// 触发翻倍的那一轮奖励（86JP SeriaLuck 契约，见下）。
type MagicBoxResult struct {
	CharacterID       string
	Success           bool
	Reason            string
	ClientType        byte
	BoxSlotIndex      int16
	BoxItemID         int64
	MaterialSlotIndex int16
	MaterialItemID    int64
	OpenCount         int64
	Rewards           []MagicBoxGrantedReward
	DoubleRewards     []MagicBoxGrantedReward
	// DisplayRewards 是 86JP GetPrimaryRewards 语义：仅基础抽取（不含翻倍
	// 轮），用于赛丽亚批量回包左栏；右栏用 DoubleRewards。两个列表重复会
	// 让客户端把左栏合并清空（2026-07-26 实测）。
	DisplayRewards []MagicBoxGrantedReward
	MainRefresh    map[string]dnfrepo.ItemStack
	// OverflowMailID identifies the system mail created when a main-inventory
	// reward cannot be placed. The packet result omits those rows because the
	// current EXE requires a real slot for every magic-box result entry.
	OverflowMailID string
	Changed        bool
	// SeriaLuckDoubleTriggered 表示本次开启至少一轮触发了赛丽亚翻倍
	// (86JP doubleFlag)；SeriaLuckBefore/After/Max 只用于日志与测试断言，
	// 不进回包。
	SeriaLuckDoubleTriggered bool
	SeriaLuckBefore          int64
	SeriaLuckAfter           int64
}

// currentSeriaLuckItemID 是 86JP SeriaLuckItemConstants.ItemTemplateId 对应
// 的当前 PVF 赛丽亚的幸运物品；只有开它才累计/触发幸运值翻倍。
const currentSeriaLuckItemID = int64(2682272)

// SeriaLuckValueMax 与 86JP SqliteAccountRepository.SeriaLuckValueMax 一致：
// 幸运值满 8 的那一轮奖励翻倍，随后清零重新累计。导出该契约供 bridge
// 把同一账号绝对值投影到当前 EXE 的 HUD gauge。
const SeriaLuckValueMax = int64(8)

// SeriaLuckMetadataKey 是账号级赛丽亚幸运值的唯一持久化键。
const SeriaLuckMetadataKey = "seria_luck_value"

const (
	currentSeriaLuckValueMax    = SeriaLuckValueMax
	currentSeriaLuckMetadataKey = SeriaLuckMetadataKey
)

// MagicBoxGrantedReward 是聚合后的实际入库奖励行（同 item 合并数量）。
// Slot 是奖励落入的主背包槽位（连开 ACK 的 0x77 展示行需要真实槽位），
// 找不到时为 -1。装备类奖励（Kind=equipment）以 QualitySeed 代替数量展示，
// 否则客户端结果窗口渲染为"加载中"（2026-07-26 实测）。
type MagicBoxGrantedReward struct {
	ItemID      int64
	Count       int64
	Slot        int16
	Kind        string
	QualitySeed uint32
	Durability  uint16
}

// ApplyMagicBox opens one magic box following the 86JP InventoryPackageStore
// contract: resolve the pool and material requirement from the runtime PVF,
// draw one weighted entry per group (DrawCount picks each), consume the box
// and the per-open material, then grant the aggregated rewards into the main
// inventory inside one character-item transaction. Only the single-open
// count is owned here; batch opens and the Seria luck doubling are
// intentionally not owned yet.
func (o *Owner) ApplyMagicBox(ctx context.Context, cmd MagicBoxCommand, boxResolver alignedcmd.MagicBoxResolver, rewardResolver alignedcmd.MagicBoxRewardItemResolver) (MagicBoxResult, error) {
	base := MagicBoxResult{
		ClientType:        cmd.RawListType,
		BoxSlotIndex:      cmd.SlotIndex,
		MaterialSlotIndex: cmd.MaterialSlotIndex,
	}
	if boxResolver == nil || rewardResolver == nil {
		return MagicBoxResult{}, ErrMagicBoxResolverRequired
	}
	if o == nil || o.inventory == nil {
		return MagicBoxResult{}, ErrOwnerUnavailable
	}
	if cmd.SelectedCharacterID == 0 {
		return MagicBoxResult{}, ErrCharacterRequired
	}
	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	base.CharacterID = characterID
	if !o.inMailboxTransaction && o.mailboxAssets != nil {
		var result MagicBoxResult
		err := o.mailboxAssets.WithinMailboxAssets(ctx, characterID, characterID, func(
			_ dnfrepo.CharacterRepository,
			inventory dnfrepo.InventoryRepository,
			mailboxes dnfrepo.MailboxRepository,
		) error {
			txOwner := *o
			txOwner.inventory = inventory
			txOwner.mailbox = mailboxes
			txOwner.inMailboxTransaction = true
			var err error
			result, err = txOwner.ApplyMagicBox(ctx, cmd, boxResolver, rewardResolver)
			return err
		})
		if err != nil {
			return MagicBoxResult{}, err
		}
		if result.CharacterID == "" {
			result = base
		}
		return result, nil
	}
	if !o.inItemTransaction && !o.inMailboxTransaction {
		if o.items == nil {
			return MagicBoxResult{}, ErrInventoryTransactionRequired
		}
		var result MagicBoxResult
		err := o.items.WithinCharacterItems(ctx, characterID, func(inventory dnfrepo.InventoryRepository, _ dnfrepo.EquipmentRepository) error {
			txOwner := *o
			txOwner.inventory = inventory
			txOwner.inItemTransaction = true
			var err error
			result, err = txOwner.ApplyMagicBox(ctx, cmd, boxResolver, rewardResolver)
			return err
		})
		if err != nil {
			return MagicBoxResult{}, err
		}
		if result.CharacterID == "" {
			result = base
		}
		return result, nil
	}

	return o.applyMagicBoxInTransaction(ctx, base, cmd, 1, 0, 0, cmd.AccountID, boxResolver, rewardResolver)
}

// ApplyMagicBoxExpand 处理 0x0468 连开（全部开启）：在同一角色物品事务里按
// 请求数、盒子堆叠数和材料可用数的最小值开启 N 次，消耗 N 个盒子与
// N×单次材料，聚合全部抽奖结果一次性入库。盒子/材料 item id 以仓库堆叠
// 为准，请求里客户端自报的 id 只作一致性校验，不匹配直接失败回包。
func (o *Owner) ApplyMagicBoxExpand(ctx context.Context, cmd MagicBoxExpandCommand, boxResolver alignedcmd.MagicBoxResolver, rewardResolver alignedcmd.MagicBoxRewardItemResolver) (MagicBoxResult, error) {
	base := MagicBoxResult{
		ClientType:        cmd.RawListType,
		BoxSlotIndex:      cmd.SlotIndex,
		MaterialSlotIndex: cmd.MaterialSlotIndex,
	}
	if boxResolver == nil || rewardResolver == nil {
		return MagicBoxResult{}, ErrMagicBoxResolverRequired
	}
	if o == nil || o.inventory == nil {
		return MagicBoxResult{}, ErrOwnerUnavailable
	}
	if cmd.SelectedCharacterID == 0 {
		return MagicBoxResult{}, ErrCharacterRequired
	}
	characterID := strconv.FormatUint(uint64(cmd.SelectedCharacterID), 10)
	base.CharacterID = characterID
	if !o.inMailboxTransaction && o.mailboxAssets != nil {
		var result MagicBoxResult
		err := o.mailboxAssets.WithinMailboxAssets(ctx, characterID, characterID, func(
			_ dnfrepo.CharacterRepository,
			inventory dnfrepo.InventoryRepository,
			mailboxes dnfrepo.MailboxRepository,
		) error {
			txOwner := *o
			txOwner.inventory = inventory
			txOwner.mailbox = mailboxes
			txOwner.inMailboxTransaction = true
			var err error
			result, err = txOwner.ApplyMagicBoxExpand(ctx, cmd, boxResolver, rewardResolver)
			return err
		})
		if err != nil {
			return MagicBoxResult{}, err
		}
		if result.CharacterID == "" {
			result = base
		}
		return result, nil
	}
	if !o.inItemTransaction && !o.inMailboxTransaction {
		if o.items == nil {
			return MagicBoxResult{}, ErrInventoryTransactionRequired
		}
		var result MagicBoxResult
		err := o.items.WithinCharacterItems(ctx, characterID, func(inventory dnfrepo.InventoryRepository, _ dnfrepo.EquipmentRepository) error {
			txOwner := *o
			txOwner.inventory = inventory
			txOwner.inItemTransaction = true
			var err error
			result, err = txOwner.ApplyMagicBoxExpand(ctx, cmd, boxResolver, rewardResolver)
			return err
		})
		if err != nil {
			return MagicBoxResult{}, err
		}
		if result.CharacterID == "" {
			result = base
		}
		return result, nil
	}

	single := MagicBoxCommand{
		SelectedCharacterID: cmd.SelectedCharacterID,
		RawListType:         cmd.RawListType,
		ListType:            cmd.ListType,
		SlotIndex:           cmd.SlotIndex,
		MaterialSlotIndex:   cmd.MaterialSlotIndex,
	}
	return o.applyMagicBoxInTransaction(ctx, base, single, int64(cmd.OpenCount), cmd.BoxItemID, cmd.MaterialItemID, cmd.AccountID, boxResolver, rewardResolver)
}

// applyMagicBoxInTransaction 是单开/连开共用的事务内核心；opens 是请求的
// 开启次数（单开=1），expectedBoxItemID/expectedMaterialItemID 非零时按
// 客户端自报值做一致性校验。accountID 用于赛丽亚幸运值的账号级读写，
// 为空时跳过 SeriaLuck 累计（奖励照常发放）。
func (o *Owner) applyMagicBoxInTransaction(
	ctx context.Context,
	result MagicBoxResult,
	cmd MagicBoxCommand,
	opens int64,
	expectedBoxItemID uint32,
	expectedMaterialItemID uint32,
	accountID string,
	boxResolver alignedcmd.MagicBoxResolver,
	rewardResolver alignedcmd.MagicBoxRewardItemResolver,
) (MagicBoxResult, error) {
	if opens < 1 {
		opens = 1
	}
	record, _, err := o.loadInventory(ctx, cmd.SelectedCharacterID)
	if err != nil {
		return MagicBoxResult{}, err
	}
	items := record.Slots
	boxKey := slotKey(cmd.ListType, cmd.SlotIndex)
	box, ok := items[boxKey]
	if !ok || box.Count <= 0 {
		result.Reason = fmt.Sprintf("box slot empty: list=%d slot=%d", cmd.ListType, cmd.SlotIndex)
		return result, nil
	}
	if expectedBoxItemID != 0 && box.ItemID != int64(expectedBoxItemID) {
		result.Reason = fmt.Sprintf("box item mismatch: slot=%d stack=%d request=%d", cmd.SlotIndex, box.ItemID, expectedBoxItemID)
		return result, nil
	}
	result.BoxItemID = box.ItemID

	resolution, err := boxResolver(box.ItemID)
	if err != nil {
		return MagicBoxResult{}, err
	}
	if resolution.Kind != "random" {
		result.Reason = fmt.Sprintf("box kind unsupported: item=%d kind=%q path=%s", box.ItemID, resolution.Kind, resolution.BoxPVFPath)
		return result, nil
	}

	var materialKey string
	var material dnfrepo.ItemStack
	if resolution.MaterialItemID > 0 && resolution.MaterialCountPerUse > 0 {
		result.MaterialItemID = resolution.MaterialItemID
		if expectedMaterialItemID != 0 && int64(expectedMaterialItemID) != resolution.MaterialItemID {
			result.Reason = fmt.Sprintf("material item mismatch: request=%d resolved=%d", expectedMaterialItemID, resolution.MaterialItemID)
			return result, nil
		}
		if cmd.MaterialSlotIndex >= 0 {
			candidateKey := slotKey(listTypeMain, cmd.MaterialSlotIndex)
			if candidate, found := items[candidateKey]; found && candidate.Count >= resolution.MaterialCountPerUse && candidate.ItemID == resolution.MaterialItemID {
				materialKey, material = candidateKey, candidate
			}
		}
		if materialKey == "" {
			keys := make([]string, 0, len(items))
			for key, stack := range items {
				if stack.ItemID == resolution.MaterialItemID && stack.Count >= resolution.MaterialCountPerUse {
					keys = append(keys, key)
				}
			}
			sort.Strings(keys)
			if len(keys) > 0 {
				materialKey = keys[0]
				material = items[materialKey]
			}
		}
		if materialKey == "" {
			result.Reason = fmt.Sprintf("material missing: item=%d need=%d", resolution.MaterialItemID, resolution.MaterialCountPerUse)
			return result, nil
		}
		result.MaterialSlotIndex = magicBoxSlotFromKey(materialKey)
	}

	// 连开按实际可开次数收敛：盒子堆叠与材料堆叠都不够时直接失败。
	if box.Count < opens {
		opens = box.Count
	}
	if materialKey != "" && material.Count/resolution.MaterialCountPerUse < opens {
		opens = material.Count / resolution.MaterialCountPerUse
	}
	if opens < 1 {
		result.Reason = fmt.Sprintf("nothing openable: box=%d material=%d perUse=%d", box.Count, material.Count, resolution.MaterialCountPerUse)
		return result, nil
	}

	// 86JP SeriaLuck：开 2682272 时按账号持久化幸运值逐轮判定，满 8 的
	// 那一轮奖励翻倍（再次加入发放表并进入 DoubleRewards），随后清零重新
	// 累计。非赛丽亚源跳过整段。
	seriaSource := box.ItemID == currentSeriaLuckItemID
	seriaLuck := int64(0)
	var account dnfrepo.AccountRecord
	accountFound := false
	if seriaSource && o.accounts != nil && accountID != "" {
		loaded, found, loadErr := o.accounts.Load(ctx, accountID)
		if loadErr != nil {
			return MagicBoxResult{}, loadErr
		}
		if found {
			account = dnfrepo.CloneAccount(loaded)
			accountFound = true
			if raw := strings.TrimSpace(account.Metadata[currentSeriaLuckMetadataKey]); raw != "" {
				if value, parseErr := strconv.ParseInt(raw, 10, 64); parseErr == nil && value >= 0 {
					seriaLuck = value
				}
			}
		}
	}
	result.SeriaLuckBefore = seriaLuck

	granted := make(map[int64]int64)
	doubleGranted := make(map[int64]int64)
	displayGranted := make(map[int64]int64)
	for open := int64(0); open < opens; open++ {
		drawn := drawMagicBoxRewards(resolution, 1)
		for itemID, count := range drawn {
			granted[itemID] += count
			displayGranted[itemID] += count
		}
		if seriaSource {
			if seriaLuck >= currentSeriaLuckValueMax {
				for itemID, count := range drawn {
					granted[itemID] += count
					doubleGranted[itemID] += count
				}
				seriaLuck = 0
			}
			if seriaLuck < currentSeriaLuckValueMax {
				seriaLuck++
			}
		}
	}
	if seriaSource {
		result.SeriaLuckAfter = seriaLuck
		result.SeriaLuckDoubleTriggered = len(doubleGranted) > 0
	}
	if len(granted) == 0 {
		result.Reason = fmt.Sprintf("empty reward pool: item=%d path=%s", box.ItemID, resolution.BoxPVFPath)
		return result, nil
	}

	rewardIDs := make([]int64, 0, len(granted))
	for itemID := range granted {
		rewardIDs = append(rewardIDs, itemID)
	}
	sort.Slice(rewardIDs, func(i, j int) bool { return rewardIDs[i] < rewardIDs[j] })
	rewardItems := make(map[int64]alignedcmd.MagicBoxRewardItem, len(rewardIDs))
	for _, itemID := range rewardIDs {
		rewardItem, err := rewardResolver(itemID)
		if err != nil {
			return MagicBoxResult{}, err
		}
		if rewardItem.ItemID <= 0 {
			result.Reason = fmt.Sprintf("reward item unresolved: %d", itemID)
			return result, nil
		}
		rewardItems[itemID] = rewardItem
	}
	overflowCounts := make(map[int64]int64)
	overflow := make([]dnfrepo.MailAttachment, 0)
	for _, itemID := range rewardIDs {
		remaining, stacks, grantErr := grantMagicBoxReward(items, rewardItems[itemID], granted[itemID])
		if grantErr != nil {
			if errors.Is(grantErr, ErrRewardInventoryFull) {
				result.Reason = grantErr.Error()
				return result, nil
			}
			return MagicBoxResult{}, grantErr
		}
		if remaining == 0 {
			continue
		}
		if !o.inMailboxTransaction || o.mailbox == nil {
			return MagicBoxResult{}, ErrMailboxTransactionRequired
		}
		overflowCounts[itemID] = remaining
		for _, stack := range stacks {
			overflow = append(overflow, dnfrepo.MailAttachment{
				ItemID:   stack.ItemID,
				Count:    stack.Count,
				Bind:     stack.Bind,
				ExpireAt: stack.ExpireAt,
				RawEntry: append([]byte(nil), stack.RawEntry...),
				Extra:    cloneMagicBoxExtra(stack.Extra),
			})
		}
	}

	box = cloneMagicBoxStack(box)
	box.Count -= opens
	if box.Count <= 0 {
		delete(items, boxKey)
	} else {
		updateMagicBoxStackRawAmount(&box)
		items[boxKey] = box
	}
	if materialKey != "" {
		material = cloneMagicBoxStack(material)
		material.Count -= resolution.MaterialCountPerUse * opens
		if material.Count <= 0 {
			delete(items, materialKey)
		} else {
			updateMagicBoxStackRawAmount(&material)
			items[materialKey] = material
		}
	}

	record.UpdatedAt = time.Now()
	if err := dnfrepo.SaveInventoryFields(ctx, o.inventory, record, dnfrepo.InventoryFieldSlots); err != nil {
		return MagicBoxResult{}, err
	}
	if len(overflow) > 0 {
		mailID, mailErr := dnfrepo.AppendSystemMail(ctx, o.mailbox, dnfrepo.SystemMailDelivery{
			RecipientCharacterID: result.CharacterID,
			Title:                "背包已满：礼盒奖励",
			Body:                 "背包空间不足，礼盒奖励已通过邮件发送。请清理对应道具分页后领取。",
			Source:               "magic_box_reward_inventory_full",
			Attachments:          overflow,
			CreatedAt:            record.UpdatedAt,
		})
		if mailErr != nil {
			return MagicBoxResult{}, mailErr
		}
		result.OverflowMailID = mailID
	}
	if seriaSource && accountFound {
		if account.Metadata == nil {
			account.Metadata = make(map[string]string, 4)
		}
		account.Metadata[currentSeriaLuckMetadataKey] = strconv.FormatInt(seriaLuck, 10)
		account.UpdatedAt = time.Now().UTC()
		// 账号保存失败则整个角色物品事务一起回滚，避免奖励已发而幸运值未记。
		if err := o.accounts.Save(ctx, account); err != nil {
			return MagicBoxResult{}, err
		}
	}
	result.Rewards = make([]MagicBoxGrantedReward, 0, len(rewardIDs))
	for _, itemID := range rewardIDs {
		delivered := granted[itemID] - overflowCounts[itemID]
		if delivered <= 0 {
			continue
		}
		result.Rewards = append(result.Rewards, magicBoxResultReward(rewardItems[itemID], delivered, magicBoxRewardSlot(record.Slots, itemID)))
	}
	if len(doubleGranted) > 0 {
		doubleIDs := make([]int64, 0, len(doubleGranted))
		for itemID := range doubleGranted {
			doubleIDs = append(doubleIDs, itemID)
		}
		sort.Slice(doubleIDs, func(i, j int) bool { return doubleIDs[i] < doubleIDs[j] })
		result.DoubleRewards = make([]MagicBoxGrantedReward, 0, len(doubleIDs))
		for _, itemID := range doubleIDs {
			if overflowCounts[itemID] > 0 {
				continue
			}
			result.DoubleRewards = append(result.DoubleRewards, magicBoxResultReward(rewardItems[itemID], doubleGranted[itemID], magicBoxRewardSlot(record.Slots, itemID)))
		}
	}
	if seriaSource {
		displayIDs := make([]int64, 0, len(displayGranted))
		for itemID := range displayGranted {
			displayIDs = append(displayIDs, itemID)
		}
		sort.Slice(displayIDs, func(i, j int) bool { return displayIDs[i] < displayIDs[j] })
		result.DisplayRewards = make([]MagicBoxGrantedReward, 0, len(displayIDs))
		for _, itemID := range displayIDs {
			if overflowCounts[itemID] > 0 {
				continue
			}
			result.DisplayRewards = append(result.DisplayRewards, magicBoxResultReward(rewardItems[itemID], displayGranted[itemID], magicBoxRewardSlot(record.Slots, itemID)))
		}
	}
	result.Success = true
	result.OpenCount = opens
	result.MainRefresh = cloneMagicBoxItemMap(record.Slots)
	result.Changed = true
	return result, nil
}

// currentMagicBoxTopQualitySeed 是 86JP ItemQuality.TopQualitySeed：礼包/魔盒
// 产出的装备统一发顶级品质种子。
const currentMagicBoxTopQualitySeed = uint32(999999998)

// magicBoxResultReward 组装一条结果奖励行；装备类按 86JP 契约带顶级品质种子
// 与 PVF 耐久（Count 恒为 1）。
func magicBoxResultReward(item alignedcmd.MagicBoxRewardItem, count int64, slot int16) MagicBoxGrantedReward {
	reward := MagicBoxGrantedReward{
		ItemID:      item.ItemID,
		Count:       count,
		Slot:        slot,
		Kind:        item.Kind,
		QualitySeed: 0,
		Durability:  item.Durability,
	}
	if item.Kind == "equipment" {
		reward.Count = 1
		reward.QualitySeed = currentMagicBoxTopQualitySeed
	}
	return reward
}

// drawMagicBoxRewards 按 86JP InventoryPackageStore 的组规则抽 opens 轮：
// 每组先算总权重，DrawCount 决定每轮每组抽几次，全部轮次聚合到同一张
// item->count 表。
func drawMagicBoxRewards(resolution alignedcmd.MagicBoxResolution, opens int64) map[int64]int64 {
	granted := make(map[int64]int64)
	if opens < 1 {
		opens = 1
	}
	for _, group := range resolution.Groups {
		totalWeight := int64(0)
		for _, entry := range group.Entries {
			if entry.Weight > 0 {
				totalWeight += entry.Weight
			}
		}
		if totalWeight <= 0 {
			continue
		}
		drawCount := group.DrawCount
		if drawCount < 1 {
			drawCount = 1
		}
		for open := int64(0); open < opens; open++ {
			for draw := int64(0); draw < drawCount; draw++ {
				roll := rand.Int63n(totalWeight)
				cumulative := int64(0)
				for _, entry := range group.Entries {
					if entry.Weight <= 0 {
						continue
					}
					cumulative += entry.Weight
					if roll >= cumulative {
						continue
					}
					count := entry.Count
					if count < 1 {
						count = 1
					}
					granted[entry.ItemID] += count
					break
				}
			}
		}
	}
	return granted
}

// grantMagicBoxReward places one aggregated reward into the main inventory:
// existing same-item-and-expiration stacks in quick slots 3-8 first, then
// ascending slots, each topped up to the PVF stack limit (0 = unlimited),
// then fresh empty slots inside the item's PVF slot range (86JP
// TryAddBoosterRewardItems order). An old expire=0 row can therefore never
// absorb a newly granted relative-period reward.
func grantMagicBoxReward(items map[string]dnfrepo.ItemStack, reward alignedcmd.MagicBoxRewardItem, count int64) (int64, []dnfrepo.ItemStack, error) {
	if count <= 0 {
		return 0, nil, nil
	}
	targetListType := reward.TargetListType
	remaining := count
	fill := func(key string) {
		if remaining <= 0 || reward.StackLimit == 1 {
			return
		}
		stack, ok := items[key]
		if !ok || stack.ItemID != reward.ItemID || stack.Count <= 0 || !magicBoxRewardExpirationMatches(stack, reward.ExpireAt) {
			return
		}
		if reward.StackLimit > 0 && stack.Count >= reward.StackLimit {
			return
		}
		capacity := remaining
		if reward.StackLimit > 0 {
			capacity = reward.StackLimit - stack.Count
		}
		if capacity <= 0 {
			return
		}
		add := remaining
		if add > capacity {
			add = capacity
		}
		stack = cloneMagicBoxStack(stack)
		stack.Count += add
		updateMagicBoxStackRawAmount(&stack)
		items[key] = stack
		remaining -= add
	}
	if targetListType == listTypeMain {
		for slot := int16(3); slot <= 8 && remaining > 0; slot++ {
			fill(slotKey(listTypeMain, slot))
		}
	}
	for slot := reward.SlotStart; slot <= reward.SlotEnd && remaining > 0; slot++ {
		fill(slotKey(targetListType, slot))
	}
	for slot := reward.SlotStart; slot <= reward.SlotEnd && remaining > 0; slot++ {
		key := slotKey(targetListType, slot)
		if _, occupied := items[key]; occupied {
			continue
		}
		insert := remaining
		if reward.StackLimit > 0 && insert > reward.StackLimit {
			insert = reward.StackLimit
		}
		if reward.Kind == "equipment" && insert > 1 {
			insert = 1
		}
		stack := dnfrepo.ItemStack{
			ItemID:   reward.ItemID,
			Count:    insert,
			ExpireAt: reward.ExpireAt,
			Extra: map[string]string{
				"item_kind": reward.Kind,
				"pvf_path":  reward.PVFPath,
			},
		}
		if reward.Kind == "equipment" {
			// 86JP ItemQuality 契约：礼包/魔盒装备统一发顶级品质种子与 PVF 耐久。
			stack.Count = 1
			stack.Extra["quality_seed"] = strconv.FormatUint(uint64(currentMagicBoxTopQualitySeed), 10)
			if reward.Durability > 0 {
				stack.Extra["durability"] = strconv.FormatUint(uint64(reward.Durability), 10)
				stack.Extra["max_durability"] = strconv.FormatUint(uint64(reward.Durability), 10)
			}
		}
		if reward.EquipmentType != "" {
			stack.Extra["equipment_type"] = reward.EquipmentType
		}
		if !reward.ExpireAt.IsZero() && reward.ExpireAt.Unix() > 0 {
			expireUnix := strconv.FormatInt(reward.ExpireAt.Unix(), 10)
			stack.Extra["expire_time"] = expireUnix
			stack.Extra["expire_unix"] = expireUnix
		}
		if reward.UsablePeriodDays > 0 {
			stack.Extra["usable_period_days"] = strconv.FormatInt(reward.UsablePeriodDays, 10)
			stack.Extra["expiration_source"] = "runtime_pvf_usable_period_grant"
		}
		if reward.Seal {
			stack.Extra["seal_flag"] = "1"
		}
		updateMagicBoxStackRawAmount(&stack)
		items[key] = stack
		remaining -= insert
	}
	if remaining == 0 {
		return 0, nil, nil
	}
	if targetListType != listTypeMain {
		return 0, nil, fmt.Errorf("%w: target_list=%d item=%d", ErrRewardInventoryFull, targetListType, reward.ItemID)
	}
	left := make([]dnfrepo.ItemStack, 0)
	for remaining > 0 {
		part := remaining
		if reward.StackLimit > 0 && part > reward.StackLimit {
			part = reward.StackLimit
		}
		if reward.Kind == "equipment" && part > 1 {
			part = 1
		}
		left = append(left, newMagicBoxOverflowStack(reward, part))
		remaining -= part
	}
	var total int64
	for _, stack := range left {
		total += stack.Count
	}
	return total, left, nil
}

func newMagicBoxOverflowStack(reward alignedcmd.MagicBoxRewardItem, count int64) dnfrepo.ItemStack {
	stack := dnfrepo.ItemStack{
		ItemID:   reward.ItemID,
		Count:    count,
		ExpireAt: reward.ExpireAt,
		Extra: map[string]string{
			"item_kind": reward.Kind,
			"pvf_path":  reward.PVFPath,
		},
	}
	if reward.Kind == "equipment" {
		stack.Count = 1
		stack.Extra["quality_seed"] = strconv.FormatUint(uint64(currentMagicBoxTopQualitySeed), 10)
		if reward.Durability > 0 {
			stack.Extra["durability"] = strconv.FormatUint(uint64(reward.Durability), 10)
			stack.Extra["max_durability"] = strconv.FormatUint(uint64(reward.Durability), 10)
		}
	}
	if reward.EquipmentType != "" {
		stack.Extra["equipment_type"] = reward.EquipmentType
	}
	if !reward.ExpireAt.IsZero() && reward.ExpireAt.Unix() > 0 {
		expireUnix := strconv.FormatInt(reward.ExpireAt.Unix(), 10)
		stack.Extra["expire_time"] = expireUnix
		stack.Extra["expire_unix"] = expireUnix
	}
	if reward.UsablePeriodDays > 0 {
		stack.Extra["usable_period_days"] = strconv.FormatInt(reward.UsablePeriodDays, 10)
		stack.Extra["expiration_source"] = "runtime_pvf_usable_period_grant"
	}
	if reward.Seal {
		stack.Extra["seal_flag"] = "1"
	}
	updateMagicBoxStackRawAmount(&stack)
	return stack
}

func magicBoxRewardExpirationMatches(stack dnfrepo.ItemStack, expiration time.Time) bool {
	expected := int64(0)
	if !expiration.IsZero() && expiration.Unix() > 0 {
		expected = expiration.Unix()
	}
	return magicBoxStackExpireUnix(stack) == expected
}

func magicBoxStackExpireUnix(stack dnfrepo.ItemStack) int64 {
	for _, key := range []string{"expire_time", "expire_unix"} {
		value, err := strconv.ParseInt(strings.TrimSpace(stack.Extra[key]), 10, 64)
		if err == nil && value > 0 {
			return value
		}
	}
	if !stack.ExpireAt.IsZero() && stack.ExpireAt.Unix() > 0 {
		return stack.ExpireAt.Unix()
	}
	if len(stack.RawEntry) == currentMagicBoxEntrySize {
		return int64(binary.LittleEndian.Uint32(stack.RawEntry[0x38:0x3C]))
	}
	return 0
}

func magicBoxSlotFromKey(key string) int16 {
	parts := strings.SplitN(key, ":", 2)
	if len(parts) != 2 {
		return -1
	}
	slot, err := strconv.ParseInt(parts[1], 10, 16)
	if err != nil {
		return -1
	}
	return int16(slot)
}

// magicBoxRewardSlot 返回主背包里持有该奖励 item 的最低槽位；连开 ACK 的
// 0x77 展示行必须引用真实槽位，否则客户端结果窗口无法定位奖励。
func magicBoxRewardSlot(items map[string]dnfrepo.ItemStack, itemID int64) int16 {
	best := int16(-1)
	for key, stack := range items {
		if stack.ItemID != itemID || stack.Count <= 0 {
			continue
		}
		parts := strings.SplitN(key, ":", 2)
		if len(parts) != 2 {
			continue
		}
		slot := magicBoxSlotFromKey(key)
		if slot < 0 {
			continue
		}
		if best < 0 || slot < best {
			best = slot
		}
	}
	return best
}

func cloneMagicBoxStack(stack dnfrepo.ItemStack) dnfrepo.ItemStack {
	out := stack
	if stack.Extra != nil {
		extra := make(map[string]string, len(stack.Extra))
		for key, value := range stack.Extra {
			extra[key] = value
		}
		out.Extra = extra
	}
	if stack.RawEntry != nil {
		out.RawEntry = append([]byte(nil), stack.RawEntry...)
	}
	return out
}

func cloneMagicBoxExtra(extra map[string]string) map[string]string {
	if len(extra) == 0 {
		return nil
	}
	out := make(map[string]string, len(extra))
	for key, value := range extra {
		out[key] = value
	}
	return out
}

func updateMagicBoxStackRawAmount(stack *dnfrepo.ItemStack) {
	if stack == nil {
		return
	}
	if stack.Extra == nil {
		stack.Extra = make(map[string]string, 4)
	}
	stack.Extra["amount_or_count"] = strconv.FormatInt(stack.Count, 10)
	stack.Extra["amount"] = strconv.FormatInt(stack.Count, 10)
	if len(stack.RawEntry) == currentMagicBoxEntrySize {
		stack.RawEntry = append([]byte(nil), stack.RawEntry...)
		value := uint32(stack.Count)
		stack.RawEntry[0x06] = byte(value)
		stack.RawEntry[0x07] = byte(value >> 8)
		stack.RawEntry[0x08] = byte(value >> 16)
		stack.RawEntry[0x09] = byte(value >> 24)
	}
}

func cloneMagicBoxItemMap(items map[string]dnfrepo.ItemStack) map[string]dnfrepo.ItemStack {
	out := make(map[string]dnfrepo.ItemStack, len(items))
	for key, stack := range items {
		out[key] = cloneMagicBoxStack(stack)
	}
	return out
}

const currentMagicBoxEntrySize = 0x77

// SelectableCommand 描述可选礼包开启前的库存预检。
type SelectableCommand struct {
	SelectedCharacterID    uint16
	SlotIndex              int16
	SelectedItemTemplateID int32
}

// MagicBoxCommand 描述随机盒子单开前的库存预检。
type MagicBoxCommand struct {
	SelectedCharacterID uint16
	AccountID           string
	RawListType         byte
	ListType            byte
	SlotIndex           int16
	MaterialSlotIndex   int16
}

// MagicBoxExpandCommand 描述 0x0468 随机盒连开请求；BoxItemID 与
// MaterialItemID 是客户端自报值，仅用于和仓库堆叠/PVF 解析结果做一致性
// 校验，不参与发放决策。
type MagicBoxExpandCommand struct {
	SelectedCharacterID uint16
	AccountID           string
	RawListType         byte
	ListType            byte
	SlotIndex           int16
	BoxItemID           uint32
	MaterialSlotIndex   int16
	MaterialItemID      uint32
	OpenCount           uint16
}

// PlanResult 描述已验证的开包输入；它不代表已经消耗物品或发放奖励。
type PlanResult struct {
	CharacterID            string
	SourceListType         byte
	SourceSlotIndex        int16
	SourceItemID           int64
	MaterialSlotIndex      int16
	MaterialItemID         int64
	SelectedItemTemplateID int64
}

// Owner 只负责礼包/随机箱的可靠边界预检，不在奖励包体闭合前提交资产变更。
type Owner struct {
	inventory            dnfrepo.InventoryRepository
	items                dnfrepo.CharacterItemUnitOfWork
	accounts             dnfrepo.AccountRepository
	mailbox              dnfrepo.MailboxRepository
	mailboxAssets        dnfrepo.MailboxAssetUnitOfWork
	inItemTransaction    bool
	inMailboxTransaction bool
}

// NewOwner 创建礼包 owner；缺少背包仓储时拒绝处理。
func NewOwner(repos dnfrepo.Group) (*Owner, error) {
	if repos.Inventory == nil {
		return nil, ErrOwnerUnavailable
	}
	return &Owner{
		inventory:     repos.Inventory,
		items:         repos.CharacterItems,
		accounts:      repos.Account,
		mailbox:       repos.Mailbox,
		mailboxAssets: repos.MailboxAssets,
	}, nil
}

// PlanSelectable 校验可选礼包源物品和选择奖励；不扣物品、不写奖励。
func (o *Owner) PlanSelectable(ctx context.Context, cmd SelectableCommand) (PlanResult, error) {
	if cmd.SelectedItemTemplateID <= 0 {
		return PlanResult{}, ErrRewardMissing
	}
	record, characterID, err := o.loadInventory(ctx, cmd.SelectedCharacterID)
	if err != nil {
		return PlanResult{}, err
	}
	stack, ok := record.Slots[slotKey(listTypeMain, cmd.SlotIndex)]
	if !ok || stack.Count <= 0 {
		return PlanResult{}, fmt.Errorf("%w: slot=%d", ErrSlotNotFound, cmd.SlotIndex)
	}
	return PlanResult{
		CharacterID:            characterID,
		SourceListType:         listTypeMain,
		SourceSlotIndex:        cmd.SlotIndex,
		SourceItemID:           stack.ItemID,
		SelectedItemTemplateID: int64(cmd.SelectedItemTemplateID),
	}, nil
}

// PlanMagicBox 校验随机箱源物品和可选材料格；不执行随机、不扣物品、不发奖。
func (o *Owner) PlanMagicBox(ctx context.Context, cmd MagicBoxCommand) (PlanResult, error) {
	record, characterID, err := o.loadInventory(ctx, cmd.SelectedCharacterID)
	if err != nil {
		return PlanResult{}, err
	}
	stack, ok := record.Slots[slotKey(cmd.ListType, cmd.SlotIndex)]
	if !ok || stack.Count <= 0 {
		return PlanResult{}, fmt.Errorf("%w: list=%d slot=%d", ErrSlotNotFound, cmd.ListType, cmd.SlotIndex)
	}
	result := PlanResult{
		CharacterID:       characterID,
		SourceListType:    cmd.ListType,
		SourceSlotIndex:   cmd.SlotIndex,
		SourceItemID:      stack.ItemID,
		MaterialSlotIndex: cmd.MaterialSlotIndex,
	}
	if cmd.MaterialSlotIndex >= 0 {
		material, ok := record.Slots[slotKey(listTypeMain, cmd.MaterialSlotIndex)]
		if !ok || material.Count <= 0 {
			return PlanResult{}, fmt.Errorf("%w: slot=%d", ErrMaterialNotFound, cmd.MaterialSlotIndex)
		}
		result.MaterialItemID = material.ItemID
	}
	return result, nil
}

func (o *Owner) loadInventory(ctx context.Context, selectedCharacterID uint16) (dnfrepo.InventoryRecord, string, error) {
	if o == nil || o.inventory == nil {
		return dnfrepo.InventoryRecord{}, "", ErrOwnerUnavailable
	}
	if selectedCharacterID == 0 {
		return dnfrepo.InventoryRecord{}, "", ErrCharacterRequired
	}
	characterID := strconv.FormatUint(uint64(selectedCharacterID), 10)
	record, ok, err := o.inventory.Load(ctx, characterID)
	if err != nil {
		return dnfrepo.InventoryRecord{}, "", err
	}
	if !ok {
		return dnfrepo.InventoryRecord{}, "", ErrInventoryNotFound
	}
	record = dnfrepo.CloneInventory(record)
	if record.Slots == nil {
		record.Slots = make(map[string]dnfrepo.ItemStack)
	}
	return record, characterID, nil
}

func slotKey(listType byte, slotIndex int16) string {
	return fmt.Sprintf("%d:%d", listType, slotIndex)
}
