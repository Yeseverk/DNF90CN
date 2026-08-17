package mail

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var (
	ErrMailboxRepoMissing         = errors.New("mailbox repository is missing")
	ErrMailboxCharacterMissing    = errors.New("mailbox character is missing")
	ErrMailboxRecipientMissing    = errors.New("mailbox recipient is missing")
	ErrMailboxInvalidRequest      = errors.New("mailbox request is invalid")
	ErrMailboxMailNotFound        = errors.New("mailbox mail is not found")
	ErrMailboxAlreadyClaimed      = errors.New("mailbox mail is already claimed")
	ErrMailboxExpired             = errors.New("mailbox mail expired")
	ErrMailboxInventoryFull       = errors.New("mailbox inventory is full")
	ErrMailboxAssetsUOW           = errors.New("mailbox asset unit of work is missing")
	ErrMailboxAssetMismatch       = errors.New("mailbox attachment does not match durable inventory")
	ErrMailboxGoldInsufficient    = errors.New("mailbox sender gold is insufficient")
	ErrMailboxDeleteClaimable     = errors.New("mailbox mail still has claimable assets")
	ErrMailboxStateUnsupported    = errors.New("mailbox state is unsupported")
	ErrMailboxSpecialUnsupported  = errors.New("special mailbox send is unsupported")
	ErrMailboxItemResolverMissing = errors.New("mailbox item price resolver is missing")
	ErrMailboxSelfRecipient       = errors.New("mailbox recipient is the sender")
	ErrMailboxSavedFull           = errors.New("mailbox saved-letter storage is full")
)

const (
	mailboxNormalPageSize = 20
	mailboxSavedPageSize  = 10
	mailboxNormalLifetime = 15 * 24 * time.Hour

	// The current PVF main-inventory categories occupy item slots 3..359:
	// quick slots 3..8, equipment 9..64, then each tab's durable range.
	mailboxMainFirstItemSlot int16 = 3
	mailboxMainLastItemSlot  int16 = 359
)

type Owner struct {
	repos        dnfrepo.Group
	itemResolver alignedcmd.MailboxItemResolver
	now          func() time.Time
}

type OpenResult struct {
	CharacterID string
	Total       int
	Unread      int
	Claimable   int
	NotLoaded   int
	Mails       []dnfrepo.MailRecord
	ObservedAt  time.Time
}

type SendResult struct {
	MailID               string
	RecipientCharacterID string
}

type ClaimResult struct {
	MailIDs           []uint32
	Gold              int64
	ItemCount         int
	InventoryOK       bool
	ItemSlotRefreshes []alignedcmd.ItemSlotRefresh
}

func NewOwner(repos dnfrepo.Group, itemResolvers ...alignedcmd.MailboxItemResolver) Owner {
	var itemResolver alignedcmd.MailboxItemResolver
	if len(itemResolvers) > 0 {
		itemResolver = itemResolvers[0]
	}
	return Owner{repos: repos, itemResolver: itemResolver, now: time.Now}
}

func (o Owner) Open(ctx context.Context, characterID string) (OpenResult, error) {
	characterID = strings.TrimSpace(characterID)
	if characterID == "" {
		return OpenResult{}, ErrMailboxCharacterMissing
	}
	if o.repos.Mailbox == nil {
		return OpenResult{}, ErrMailboxRepoMissing
	}
	record, _, err := o.loadOrCreateMailbox(ctx, characterID)
	if err != nil {
		return OpenResult{}, err
	}
	// Older claim implementations retained transferred assets on a claimed
	// letter. A claimed mail is an empty, read receipt: repair those legacy
	// rows before projecting the mailbox so the client cannot render or select
	// a stale attachment again.
	if clearClaimedMailboxAssets(&record) {
		record.UpdatedAt = o.clock()
		if err := dnfrepo.SaveMailboxFields(ctx, o.repos.Mailbox, record, dnfrepo.MailboxFieldMails); err != nil {
			return OpenResult{}, err
		}
	}
	result := OpenResult{CharacterID: characterID}
	now := o.clock()
	result.ObservedAt = now
	normal := make([]dnfrepo.MailRecord, 0, len(record.Mails))
	saved := make([]dnfrepo.MailRecord, 0, len(record.Mails))
	for _, mail := range record.Mails {
		if mail.Deleted || mailExpired(mail, now) {
			continue
		}
		result.Total++
		if !mail.Read {
			result.Unread++
		}
		if !mail.Claimed && (mail.Gold > 0 || len(mail.Attachments) > 0) {
			result.Claimable++
		}
		if mailboxMailSaved(mail) {
			saved = append(saved, cloneMailboxMail(mail))
		} else {
			normal = append(normal, cloneMailboxMail(mail))
		}
	}
	sortMailboxMails(normal)
	sortMailboxMails(saved)
	if len(normal) > mailboxNormalPageSize {
		result.NotLoaded += len(normal) - mailboxNormalPageSize
		normal = normal[:mailboxNormalPageSize]
	}
	if len(saved) > mailboxSavedPageSize {
		result.NotLoaded += len(saved) - mailboxSavedPageSize
		saved = saved[:mailboxSavedPageSize]
	}
	result.Mails = append(normal, saved...)
	return result, nil
}

func (o Owner) Send(ctx context.Context, senderCharacterID string, req SendRequest) (SendResult, error) {
	senderCharacterID = strings.TrimSpace(senderCharacterID)
	if senderCharacterID == "" {
		return SendResult{}, ErrMailboxCharacterMissing
	}
	if strings.TrimSpace(req.RecipientName) == "" {
		return SendResult{}, ErrMailboxInvalidRequest
	}
	if req.Special != 0 || req.Global {
		return SendResult{}, ErrMailboxSpecialUnsupported
	}
	if req.Gold > math.MaxInt32 {
		return SendResult{}, ErrMailboxInvalidRequest
	}
	if o.repos.Character == nil {
		return SendResult{}, ErrMailboxCharacterMissing
	}
	if o.repos.Mailbox == nil {
		return SendResult{}, ErrMailboxRepoMissing
	}
	if o.repos.MailboxAssets == nil {
		return SendResult{}, ErrMailboxAssetsUOW
	}

	recipientID, found, err := o.repos.Character.FindIDByName(ctx, req.RecipientName)
	if err != nil {
		return SendResult{}, err
	}
	if !found {
		return SendResult{}, ErrMailboxRecipientMissing
	}
	if recipientID == senderCharacterID {
		return SendResult{}, ErrMailboxSelfRecipient
	}
	var mailID string
	err = o.repos.MailboxAssets.WithinMailboxAssets(
		ctx,
		senderCharacterID,
		recipientID,
		func(
			characters dnfrepo.CharacterRepository,
			inventories dnfrepo.InventoryRepository,
			mailboxes dnfrepo.MailboxRepository,
		) error {
			sender, senderFound, loadErr := characters.Load(ctx, senderCharacterID)
			if loadErr != nil {
				return loadErr
			}
			if !senderFound {
				return ErrMailboxCharacterMissing
			}
			if sender.Stats == nil {
				sender.Stats = make(map[string]int64)
			}
			gold := int64(req.Gold)

			var inventory dnfrepo.InventoryRecord
			var inventoryFound bool
			if len(req.Attachments) > 0 {
				inventory, inventoryFound, loadErr = inventories.Load(ctx, senderCharacterID)
				if loadErr != nil {
					return loadErr
				}
				if !inventoryFound || inventory.Slots == nil {
					return ErrMailboxAssetMismatch
				}
			}
			attachments := make([]dnfrepo.MailAttachment, 0, len(req.Attachments))
			var attachmentValue int64
			for idx, requested := range req.Attachments {
				if requested.Count > math.MaxInt32 {
					return ErrMailboxInvalidRequest
				}
				key := fmt.Sprintf("%d:%d", requested.ListType, requested.SlotIndex)
				stack, ok := inventory.Slots[key]
				if !ok || stack.ItemID != int64(requested.ItemID) || stack.Count < int64(requested.Count) {
					return fmt.Errorf("%w: attachment=%d list=%d slot=%d", ErrMailboxAssetMismatch, idx, requested.ListType, requested.SlotIndex)
				}
				if o.itemResolver == nil {
					return ErrMailboxItemResolverMissing
				}
				resolution, resolveErr := o.itemResolver(requested.ItemID)
				if resolveErr != nil || resolution.Price < 0 {
					return fmt.Errorf("%w: attachment=%d item=%d resolve=%v", ErrMailboxAssetMismatch, idx, requested.ItemID, resolveErr)
				}
				count := int64(requested.Count)
				if resolution.Price > 0 && count > math.MaxInt64/resolution.Price {
					return ErrMailboxInvalidRequest
				}
				value := resolution.Price * count
				if attachmentValue > math.MaxInt64-value {
					return ErrMailboxInvalidRequest
				}
				attachmentValue += value
				attachment := dnfrepo.MailAttachment{
					ItemID:   stack.ItemID,
					Count:    count,
					Bind:     stack.Bind,
					ExpireAt: stack.ExpireAt,
					RawEntry: append([]byte(nil), stack.RawEntry...),
					Extra:    cloneStringMap(stack.Extra),
				}
				if attachment.Extra == nil {
					attachment.Extra = make(map[string]string)
				}
				attachment.Extra["mailbox_pvf_kind"] = resolution.Kind
				attachment.Extra["mailbox_equipment_type"] = resolution.EquipmentType
				attachment.Extra["mailbox_pvf_path"] = resolution.PVFPath
				attachments = append(attachments, attachment)
				if stack.Count == count {
					delete(inventory.Slots, key)
				} else {
					stack.Count -= count
					inventory.Slots[key] = stack
				}
			}
			postage, postageErr := currentMailboxPostage(gold, len(attachments), attachmentValue)
			if postageErr != nil || gold > math.MaxInt64-postage {
				return ErrMailboxInvalidRequest
			}
			totalDebit := gold + postage
			if sender.Stats["gold"] < totalDebit {
				return ErrMailboxGoldInsufficient
			}

			record, recordFound, loadErr := mailboxes.Load(ctx, recipientID)
			if loadErr != nil {
				return loadErr
			}
			if !recordFound {
				record = dnfrepo.MailboxRecord{CharacterID: recipientID}
			}
			if record.Mails == nil {
				record.Mails = make(map[string]dnfrepo.MailRecord)
			}
			mailID, loadErr = nextMailID(record.Mails)
			if loadErr != nil {
				return loadErr
			}
			now := o.clock()
			record.Mails[mailID] = dnfrepo.MailRecord{
				MailID:               mailID,
				SenderCharacterID:    senderCharacterID,
				SenderName:           sender.Name,
				RecipientCharacterID: recipientID,
				RecipientName:        req.RecipientName,
				Body:                 req.Body,
				Gold:                 gold,
				Attachments:          attachments,
				CreatedAt:            now,
				ExpireAt:             now.Add(mailboxNormalLifetime),
				Metadata: map[string]string{
					"source":       mailboxSendSource(req),
					"special":      strconv.FormatUint(uint64(req.Special), 10),
					"global":       strconv.FormatBool(req.Global),
					"postage_gold": strconv.FormatInt(postage, 10),
				},
			}
			record.UpdatedAt = now
			sender.Stats["gold"] -= totalDebit
			if err := dnfrepo.SaveCharacterFields(ctx, characters, sender, dnfrepo.CharacterFieldStats); err != nil {
				return err
			}
			if len(req.Attachments) > 0 {
				if err := dnfrepo.SaveInventoryFields(ctx, inventories, inventory, dnfrepo.InventoryFieldSlots); err != nil {
					return err
				}
			}
			return dnfrepo.SaveMailboxFields(ctx, mailboxes, record, dnfrepo.MailboxFieldMails)
		},
	)
	if err != nil {
		return SendResult{}, err
	}
	return SendResult{MailID: mailID, RecipientCharacterID: recipientID}, nil
}

func (o Owner) Claim(ctx context.Context, characterID string, req ExtractRequest) (ClaimResult, error) {
	characterID = strings.TrimSpace(characterID)
	if characterID == "" {
		return ClaimResult{}, ErrMailboxCharacterMissing
	}
	if o.repos.Mailbox == nil {
		return ClaimResult{}, ErrMailboxRepoMissing
	}
	if o.repos.MailboxAssets == nil {
		return ClaimResult{}, ErrMailboxAssetsUOW
	}
	if len(req.MailIDs) == 0 {
		return ClaimResult{}, ErrMailboxInvalidRequest
	}

	record, found, err := o.repos.Mailbox.Load(ctx, characterID)
	if err != nil {
		return ClaimResult{}, err
	}
	if !found {
		return ClaimResult{}, ErrMailboxMailNotFound
	}
	hasAttachments := false
	for _, id := range req.MailIDs {
		mail, ok := record.Mails[strconv.FormatUint(uint64(id), 10)]
		if ok && !mail.Claimed && len(mail.Attachments) > 0 {
			hasAttachments = true
			break
		}
	}
	if hasAttachments && o.itemResolver == nil {
		return ClaimResult{}, ErrMailboxItemResolverMissing
	}
	result := ClaimResult{}
	if err := o.repos.MailboxAssets.WithinMailboxAssets(ctx, characterID, characterID, func(
		characters dnfrepo.CharacterRepository,
		inventories dnfrepo.InventoryRepository,
		mailboxes dnfrepo.MailboxRepository,
	) error {
		mailbox, mailboxFound, err := mailboxes.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !mailboxFound {
			return ErrMailboxMailNotFound
		}
		now := o.clock()
		type claimCandidate struct {
			id   uint32
			key  string
			mail dnfrepo.MailRecord
		}
		selected := make([]claimCandidate, 0, len(req.MailIDs))
		for _, id := range req.MailIDs {
			key := strconv.FormatUint(uint64(id), 10)
			mail, ok := mailbox.Mails[key]
			if !ok || mail.Deleted {
				return ErrMailboxMailNotFound
			}
			if mail.Claimed {
				// The current mailbox UI keeps claimed letters visible and its
				// select-all action includes them again. Ignore those completed
				// rows so one old selection cannot block still-pending rewards in
				// the same op95 batch.
				continue
			}
			if mailExpired(mail, now) {
				return ErrMailboxExpired
			}
			if mail.Gold < 0 {
				return ErrMailboxInvalidRequest
			}
			selected = append(selected, claimCandidate{id: id, key: key, mail: mail})
		}
		if len(selected) == 0 {
			return ErrMailboxAlreadyClaimed
		}

		selectedHasAttachments := false
		for _, candidate := range selected {
			if len(candidate.mail.Attachments) > 0 {
				selectedHasAttachments = true
				break
			}
		}
		inventory := dnfrepo.InventoryRecord{CharacterID: characterID}
		if selectedHasAttachments {
			loadedInventory, inventoryFound, loadErr := inventories.Load(ctx, characterID)
			if loadErr != nil {
				return loadErr
			}
			if !inventoryFound {
				inventory = dnfrepo.InventoryRecord{CharacterID: characterID}
			} else {
				inventory = loadedInventory
			}
			if inventory.Slots == nil {
				inventory.Slots = make(map[string]dnfrepo.ItemStack)
			}
		}

		// Select-all requests contain the current page's mail IDs in order. Each
		// letter remains an atomic asset transfer, but a full tab on one letter
		// must not roll back mail that already fit into the quick slots or its
		// real item tab earlier in the same request.
		accepted := make([]claimCandidate, 0, len(selected))
		var totalGold int64
		inventoryChanged := false
		inventoryFull := false
		for _, candidate := range selected {
			candidateInventory := dnfrepo.CloneInventory(inventory)
			if candidateInventory.Slots == nil {
				candidateInventory.Slots = make(map[string]dnfrepo.ItemStack)
			}
			candidateRefreshes := make([]alignedcmd.ItemSlotRefresh, 0, len(candidate.mail.Attachments))
			candidateItemCount := 0
			fits := true
			for _, attachment := range candidate.mail.Attachments {
				if attachment.ItemID <= 0 || attachment.Count <= 0 {
					return ErrMailboxInvalidRequest
				}
				resolution, resolveErr := o.itemResolver(uint32(attachment.ItemID))
				if resolveErr != nil || !mailboxClaimSlotRangeValid(resolution) {
					return fmt.Errorf("%w: attachment item=%d resolve=%v", ErrMailboxAssetMismatch, attachment.ItemID, resolveErr)
				}
				changedSlots, addErr := addAttachmentToInventory(&candidateInventory, attachment, resolution)
				if errors.Is(addErr, ErrMailboxInventoryFull) {
					fits = false
					inventoryFull = true
					break
				}
				if addErr != nil {
					return addErr
				}
				for _, slot := range changedSlots {
					candidateRefreshes = append(candidateRefreshes, alignedcmd.ItemSlotRefresh{
						ListType:  dnfrepo.MainInventoryListType,
						SlotIndex: slot,
					})
				}
				candidateItemCount++
			}
			if !fits {
				continue
			}
			if totalGold > math.MaxInt64-candidate.mail.Gold {
				return ErrMailboxInvalidRequest
			}
			totalGold += candidate.mail.Gold
			inventory = candidateInventory
			inventoryChanged = inventoryChanged || candidateItemCount > 0
			result.ItemCount += candidateItemCount
			result.ItemSlotRefreshes = append(result.ItemSlotRefreshes, candidateRefreshes...)
			result.MailIDs = append(result.MailIDs, candidate.id)
			accepted = append(accepted, candidate)
		}
		if len(accepted) == 0 {
			if inventoryFull {
				return ErrMailboxInventoryFull
			}
			return ErrMailboxAlreadyClaimed
		}
		result.Gold = totalGold

		if totalGold > 0 {
			character, ok, err := characters.Load(ctx, characterID)
			if err != nil {
				return err
			}
			if !ok {
				return ErrMailboxCharacterMissing
			}
			if character.Stats == nil {
				character.Stats = make(map[string]int64)
			}
			if character.Stats["gold"] > math.MaxInt64-totalGold {
				return ErrMailboxInvalidRequest
			}
			character.Stats["gold"] += totalGold
			if err := dnfrepo.SaveCharacterFields(ctx, characters, character, dnfrepo.CharacterFieldStats); err != nil {
				return err
			}
		}

		if inventoryChanged {
			if err := dnfrepo.SaveInventoryFields(ctx, inventories, inventory, dnfrepo.InventoryFieldSlots); err != nil {
				return err
			}
		}

		for _, candidate := range accepted {
			mail := candidate.mail
			// Attachments and gold have already been committed to the recipient's
			// character/inventory in this same unit of work. Do not retain a second
			// claimable projection on the mail itself.
			mail.Gold = 0
			mail.Attachments = nil
			mail.Claimed = true
			mail.Read = true
			mailbox.Mails[candidate.key] = mail
		}
		mailbox.UpdatedAt = now
		return dnfrepo.SaveMailboxFields(ctx, mailboxes, mailbox, dnfrepo.MailboxFieldMails)
	}); err != nil {
		return ClaimResult{}, err
	}
	result.InventoryOK = true
	return result, nil
}

func (o Owner) ChangeState(ctx context.Context, characterID string, req ChangeStateRequest) error {
	characterID = strings.TrimSpace(characterID)
	if characterID == "" {
		return ErrMailboxCharacterMissing
	}
	if o.repos.Mailbox == nil {
		return ErrMailboxRepoMissing
	}
	if o.repos.MailboxAssets == nil {
		return ErrMailboxAssetsUOW
	}
	if req.Status != 0 && req.Status != 2 && req.Status != 3 {
		return fmt.Errorf("%w: %d", ErrMailboxStateUnsupported, req.Status)
	}
	return o.repos.MailboxAssets.WithinMailboxAssets(ctx, characterID, characterID, func(
		_ dnfrepo.CharacterRepository,
		_ dnfrepo.InventoryRepository,
		mailboxes dnfrepo.MailboxRepository,
	) error {
		record, found, err := mailboxes.Load(ctx, characterID)
		if err != nil {
			return err
		}
		if !found || len(record.Mails) == 0 {
			return ErrMailboxMailNotFound
		}
		now := o.clock()
		savedCount := 0
		for _, candidate := range record.Mails {
			if !candidate.Deleted && !mailExpired(candidate, now) && mailboxMailSaved(candidate) {
				savedCount++
			}
		}
		for _, id := range req.MailIDs {
			key := strconv.FormatUint(uint64(id), 10)
			mail, ok := record.Mails[key]
			if !ok || mail.Deleted || mailExpired(mail, now) {
				return ErrMailboxMailNotFound
			}
			switch req.Status {
			case 0:
				if !mail.Claimed && (mail.Gold > 0 || len(mail.Attachments) > 0) {
					return ErrMailboxDeleteClaimable
				}
				mail.Deleted = true
			case 2:
				mail.Read = true
			case 3:
				if !mailboxMailSaved(mail) && savedCount >= mailboxSavedPageSize {
					return ErrMailboxSavedFull
				}
				if mail.Metadata == nil {
					mail.Metadata = make(map[string]string)
				}
				if !mailboxMailSaved(mail) {
					savedCount++
				}
				mail.Metadata["mailbox_saved"] = "true"
				mail.Read = true
			}
			record.Mails[key] = mail
		}
		record.UpdatedAt = now
		return dnfrepo.SaveMailboxFields(ctx, mailboxes, record, dnfrepo.MailboxFieldMails)
	})
}

func (o Owner) loadOrCreateMailbox(ctx context.Context, characterID string) (dnfrepo.MailboxRecord, bool, error) {
	record, found, err := o.repos.Mailbox.Load(ctx, characterID)
	if err != nil {
		return dnfrepo.MailboxRecord{}, false, err
	}
	if found {
		if record.Mails == nil {
			record.Mails = make(map[string]dnfrepo.MailRecord)
		}
		return record, true, nil
	}
	record = dnfrepo.MailboxRecord{
		CharacterID: characterID,
		Mails:       make(map[string]dnfrepo.MailRecord),
		UpdatedAt:   o.clock(),
	}
	if err := o.repos.Mailbox.Save(ctx, record); err != nil {
		return dnfrepo.MailboxRecord{}, false, err
	}
	return record, false, nil
}

func clearClaimedMailboxAssets(record *dnfrepo.MailboxRecord) bool {
	if record == nil || len(record.Mails) == 0 {
		return false
	}
	changed := false
	for key, mail := range record.Mails {
		if !mail.Claimed || (mail.Gold == 0 && len(mail.Attachments) == 0) {
			continue
		}
		mail.Gold = 0
		mail.Attachments = nil
		record.Mails[key] = mail
		changed = true
	}
	return changed
}

func (o Owner) clock() time.Time {
	if o.now == nil {
		return time.Now().UTC()
	}
	return o.now().UTC()
}

func addAttachmentToInventory(inventory *dnfrepo.InventoryRecord, attachment dnfrepo.MailAttachment, resolution alignedcmd.MailboxItemResolution) ([]int16, error) {
	if inventory == nil || attachment.ItemID <= 0 || attachment.Count <= 0 || !mailboxClaimSlotRangeValid(resolution) {
		return nil, ErrMailboxInvalidRequest
	}
	if inventory.Slots == nil {
		inventory.Slots = make(map[string]dnfrepo.ItemStack)
	}
	remaining := attachment.Count
	stack := mailboxAttachmentStack(attachment, remaining)
	changedSlots := make([]int16, 0, 1)
	for _, slot := range mailboxClaimStackSlots(resolution) {
		if remaining <= 0 {
			break
		}
		key := mailboxMainSlotKey(slot)
		existing, occupied := inventory.Slots[key]
		if !occupied || !canMailStack(existing, stack) || existing.Count < 0 {
			continue
		}
		capacity := remaining
		if resolution.StackLimit > 0 && len(stack.RawEntry) == 0 {
			if existing.Count >= resolution.StackLimit {
				continue
			}
			capacity = resolution.StackLimit - existing.Count
			if capacity > remaining {
				capacity = remaining
			}
		}
		if capacity <= 0 || existing.Count > math.MaxInt64-capacity {
			return nil, ErrMailboxInvalidRequest
		}
		existing.Count += capacity
		inventory.Slots[key] = existing
		changedSlots = append(changedSlots, slot)
		remaining -= capacity
	}
	for _, slot := range mailboxClaimStackSlots(resolution) {
		if remaining <= 0 {
			break
		}
		key := mailboxMainSlotKey(slot)
		if _, occupied := inventory.Slots[key]; occupied {
			continue
		}
		count := remaining
		if resolution.StackLimit > 0 && len(stack.RawEntry) == 0 && count > resolution.StackLimit {
			count = resolution.StackLimit
		}
		inventory.Slots[key] = mailboxAttachmentStack(attachment, count)
		changedSlots = append(changedSlots, slot)
		remaining -= count
	}
	if remaining > 0 {
		return nil, ErrMailboxInventoryFull
	}
	return changedSlots, nil
}

func mailboxClaimSlotRangeValid(resolution alignedcmd.MailboxItemResolution) bool {
	return resolution.SlotStart >= mailboxMainFirstItemSlot &&
		resolution.SlotEnd >= resolution.SlotStart &&
		resolution.SlotEnd <= mailboxMainLastItemSlot
}

func mailboxClaimStackSlots(resolution alignedcmd.MailboxItemResolution) []int16 {
	slots := make([]int16, 0, 6+int(resolution.SlotEnd-resolution.SlotStart+1))
	if resolution.StackLimit != 1 {
		for slot := int16(3); slot <= 8; slot++ {
			slots = append(slots, slot)
		}
	}
	for slot := resolution.SlotStart; slot <= resolution.SlotEnd; slot++ {
		if slot < 3 || slot > 8 {
			slots = append(slots, slot)
		}
	}
	return slots
}

func mailboxAttachmentStack(attachment dnfrepo.MailAttachment, count int64) dnfrepo.ItemStack {
	extra := cloneStringMap(attachment.Extra)
	// These three keys are mailbox-envelope framing evidence captured when a
	// player sends an item. They are not durable item state, so retaining them
	// would prevent a returned stackable from merging with the same item in the
	// recipient's real inventory page.
	delete(extra, "mailbox_pvf_kind")
	delete(extra, "mailbox_equipment_type")
	delete(extra, "mailbox_pvf_path")
	return dnfrepo.ItemStack{
		ItemID:   attachment.ItemID,
		Count:    count,
		Bind:     attachment.Bind,
		ExpireAt: attachment.ExpireAt,
		RawEntry: append([]byte(nil), attachment.RawEntry...),
		Extra:    extra,
	}
}

func mailboxMainSlotKey(slot int16) string {
	return fmt.Sprintf("%d:%d", dnfrepo.MainInventoryListType, slot)
}

func canMailStack(left, right dnfrepo.ItemStack) bool {
	return left.ItemID == right.ItemID &&
		left.Bind == right.Bind &&
		left.ExpireAt.Equal(right.ExpireAt) &&
		reflect.DeepEqual(left.Extra, right.Extra) &&
		len(left.RawEntry) == 0 &&
		len(right.RawEntry) == 0
}

func nextMailID(mails map[string]dnfrepo.MailRecord) (string, error) {
	var max uint64
	for key := range mails {
		value, err := strconv.ParseUint(strings.TrimSpace(key), 10, 32)
		if err != nil {
			continue
		}
		if value > max {
			max = value
		}
	}
	if max >= math.MaxUint32 {
		return "", ErrMailboxInvalidRequest
	}
	return strconv.FormatUint(max+1, 10), nil
}

func mailboxSendSource(req SendRequest) string {
	if len(req.Attachments) > 1 {
		return "op315"
	}
	return "op94"
}

func currentMailboxPostage(gold int64, attachmentCount int, attachmentValue int64) (int64, error) {
	if gold < 0 || attachmentCount < 0 || attachmentValue < 0 {
		return 0, ErrMailboxInvalidRequest
	}
	if attachmentCount > (math.MaxInt64-100)/1000 {
		return 0, ErrMailboxInvalidRequest
	}
	postage := int64(100 + attachmentCount*1000)
	for _, surcharge := range []int64{gold / 20, attachmentValue / 20} {
		if postage > math.MaxInt64-surcharge {
			return 0, ErrMailboxInvalidRequest
		}
		postage += surcharge
	}
	return postage, nil
}

func mailExpired(mail dnfrepo.MailRecord, now time.Time) bool {
	if mailboxMailSaved(mail) || now.IsZero() {
		return false
	}
	expiresAt := mail.ExpireAt
	if expiresAt.IsZero() && !mail.CreatedAt.IsZero() {
		expiresAt = mail.CreatedAt.Add(mailboxNormalLifetime)
	}
	return !expiresAt.IsZero() && now.After(expiresAt)
}

func mailboxMailSaved(mail dnfrepo.MailRecord) bool {
	return strings.EqualFold(strings.TrimSpace(mail.Metadata["mailbox_saved"]), "true")
}

func sortMailboxMails(mails []dnfrepo.MailRecord) {
	sort.SliceStable(mails, func(i, j int) bool {
		left, right := mails[i], mails[j]
		if !left.CreatedAt.Equal(right.CreatedAt) {
			return left.CreatedAt.Before(right.CreatedAt)
		}
		leftID, leftErr := strconv.ParseUint(left.MailID, 10, 32)
		rightID, rightErr := strconv.ParseUint(right.MailID, 10, 32)
		if leftErr == nil && rightErr == nil {
			return leftID < rightID
		}
		return left.MailID < right.MailID
	})
}

func cloneMailboxMail(in dnfrepo.MailRecord) dnfrepo.MailRecord {
	out := in
	out.Attachments = append([]dnfrepo.MailAttachment(nil), in.Attachments...)
	for index := range out.Attachments {
		out.Attachments[index].RawEntry = append([]byte(nil), in.Attachments[index].RawEntry...)
		out.Attachments[index].Extra = cloneStringMap(in.Attachments[index].Extra)
	}
	out.Metadata = cloneStringMap(in.Metadata)
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func characterIDFromRequest(reqCharacterID uint16) (string, error) {
	if reqCharacterID == 0 {
		return "", ErrMailboxCharacterMissing
	}
	return strconv.FormatUint(uint64(reqCharacterID), 10), nil
}

func mailboxErrorCode(err error) byte {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, ErrMailboxRecipientMissing):
		return 2
	case errors.Is(err, ErrMailboxMailNotFound):
		return 3
	case errors.Is(err, ErrMailboxAlreadyClaimed):
		return 4
	case errors.Is(err, ErrMailboxExpired):
		return 5
	case errors.Is(err, ErrMailboxInventoryFull):
		return 6
	case errors.Is(err, ErrMailboxGoldInsufficient), errors.Is(err, ErrMailboxAssetMismatch):
		return 7
	case errors.Is(err, ErrMailboxSelfRecipient):
		return 7
	case errors.Is(err, ErrMailboxSavedFull):
		return 22
	case errors.Is(err, ErrMailboxItemResolverMissing), errors.Is(err, ErrMailboxSpecialUnsupported):
		return 7
	default:
		return 1
	}
}

func isMissingRepositoryError(err error) bool {
	return errors.Is(err, ErrMailboxRepoMissing) ||
		errors.Is(err, ErrMailboxAssetsUOW) ||
		errors.Is(err, dnfrepo.ErrMailboxAssetTransactionUnavailable)
}
