package player

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// MailRecord 是玩家邮箱中的单封邮件记录。
type MailRecord struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"created_at"`
}

// AddCurrency 增减玩家指定货币并记录关键字段变更事件。
func (m *Module) AddCurrency(ctx context.Context, accountID, currency string, delta int64) (Profile, error) {
	currency = strings.TrimSpace(currency)
	if currency == "" {
		return Profile{}, fmt.Errorf("currency is required")
	}
	return m.mutateCritProfile(ctx, accountID, ProfileOperationAddCurrency, "handler.add_currency", func(profile *Profile, _ time.Time) (map[string]any, error) {
		if profile.Currencies == nil {
			profile.Currencies = defaultCurrencies()
		}
		before := profile.Currencies[currency]
		profile.Currencies[currency] += delta
		return map[string]any{
			"currency":       currency,
			"delta":          delta,
			"balance_before": before,
			"balance_after":  profile.Currencies[currency],
		}, nil
	})
}

// SetInventoryItem 设置玩家背包物品数量，数量不大于零时删除该物品。
func (m *Module) SetInventoryItem(ctx context.Context, accountID, itemID string, quantity int64) (Profile, error) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return Profile{}, fmt.Errorf("item id is required")
	}
	return m.mutateCritProfile(ctx, accountID, ProfileOperationSetInventoryItem, "handler.set_inventory_item", func(profile *Profile, _ time.Time) (map[string]any, error) {
		if profile.Inventory == nil {
			profile.Inventory = defaultInventory()
		}
		before := profile.Inventory[itemID]
		if quantity <= 0 {
			delete(profile.Inventory, itemID)
		} else {
			profile.Inventory[itemID] = quantity
		}
		return map[string]any{
			"item_id":         itemID,
			"quantity_before": before,
			"quantity_after":  profile.Inventory[itemID],
			"removed":         quantity <= 0,
		}, nil
	})
}

// SetQuestState 设置玩家任务状态并写入任务字段。
func (m *Module) SetQuestState(ctx context.Context, accountID, questID, state string) (Profile, error) {
	questID = strings.TrimSpace(questID)
	state = strings.TrimSpace(state)
	if questID == "" {
		return Profile{}, fmt.Errorf("quest id is required")
	}
	return m.mutateCritProfile(ctx, accountID, ProfileOperationSetQuestState, "handler.set_quest_state", func(profile *Profile, _ time.Time) (map[string]any, error) {
		if profile.Quests == nil {
			profile.Quests = defaultQuests()
		}
		before := profile.Quests[questID]
		profile.Quests[questID] = state
		return map[string]any{
			"quest_id":     questID,
			"state_before": before,
			"state_after":  state,
		}, nil
	})
}

// MarkMailRead 标记玩家邮件已读，并返回是否找到和是否发生变更。
func (m *Module) MarkMailRead(ctx context.Context, accountID, mailID string) (Profile, bool, bool, error) {
	accountID = strings.TrimSpace(accountID)
	mailID = strings.TrimSpace(mailID)
	if accountID == "" {
		return Profile{}, false, false, fmt.Errorf("account id is required")
	}
	if mailID == "" {
		return Profile{}, false, false, fmt.Errorf("mail id is required")
	}
	now := time.Now().UTC()

	var activeFound, activeChanged bool
	if before, profile, ok, err := m.profiles.MutateActive(accountID, func(profile *Profile, now time.Time) ([]ProfileField, error) {
		activeFound, activeChanged = markMailRead(profile, mailID)
		if activeChanged {
			fields := dirtyFieldsByModule(ProfileModuleMailbox)
			eventProfile := cloneProfile(*profile)
			eventProfile.UpdatedAt = now
			eventProfile.Version++
			if err := m.emitProfileMutation(ctx, eventProfile, "read_mail", "handler.read_mail", fields, map[string]any{
				"mail_id":      mailID,
				"changed":      true,
				"unread_count": unreadMailCount(eventProfile),
			}); err != nil {
				return nil, err
			}
			return fields, nil
		}
		return nil, nil
	}); err != nil {
		return Profile{}, false, false, err
	} else if ok {
		if activeChanged {
			m.observeSummaryChange(ctx, before, profile)
		}
		return cloneProfile(profile), activeFound, activeChanged, nil
	}

	profile, ok, err := m.profiles.LoadDetached(ctx, accountID)
	if err != nil {
		return Profile{}, false, false, err
	}
	dirtyFields := newProfileFieldSet(dirtyFieldsByModule(ProfileModuleRuntime)...)
	if !ok {
		profile = m.profiles.NewDetached(accountID, now)
		dirtyFields.AddAll()
	} else {
		profile = normAccountID(profile, accountID)
		dirtyFields.Merge(ensureProfileModules(&profile, now))
	}
	found, changed := markMailRead(&profile, mailID)
	if !found {
		return cloneProfile(profile), false, false, nil
	}

	profile.State = "online"
	profile.LoadedAt = now
	profile.UpdatedAt = now
	profile.Version++
	if changed {
		dirtyFields.Add(dirtyFieldsByModule(ProfileModuleMailbox)...)
	}

	attachedProfile, attached := m.profiles.AttachIfAbsent(accountID, profile, dirtyFields.List()...)
	if !attached {
		_, current, ok, err := m.profiles.MutateActive(accountID, func(profile *Profile, now time.Time) ([]ProfileField, error) {
			found, changed = markMailRead(profile, mailID)
			if changed {
				fields := dirtyFieldsByModule(ProfileModuleMailbox)
				eventProfile := cloneProfile(*profile)
				eventProfile.UpdatedAt = now
				eventProfile.Version++
				if err := m.emitProfileMutation(ctx, eventProfile, "read_mail", "handler.read_mail", fields, map[string]any{
					"mail_id":      mailID,
					"changed":      true,
					"unread_count": unreadMailCount(eventProfile),
				}); err != nil {
					return nil, err
				}
				return fields, nil
			}
			return nil, nil
		})
		if err != nil {
			return Profile{}, false, false, err
		}
		if ok && changed {
			m.observeSummary(ctx, current)
		}
		return cloneProfile(current), found, changed, nil
	}
	m.observeSummary(ctx, attachedProfile)
	if changed {
		if err := m.emitProfileMutation(ctx, attachedProfile, "read_mail", "handler.read_mail", dirtyFieldsByModule(ProfileModuleMailbox), map[string]any{
			"mail_id":      mailID,
			"changed":      true,
			"unread_count": unreadMailCount(attachedProfile),
		}); err != nil {
			return cloneProfile(attachedProfile), true, changed, err
		}
	}

	return cloneProfile(attachedProfile), true, changed, nil
}

func applyProfileDefaults(profile *Profile, now time.Time) {
	if profile == nil {
		return
	}
	if strings.TrimSpace(profile.Name) == "" {
		profile.Name = "Player " + profile.AccountID
	}
	if profile.Level <= 0 {
		profile.Level = 1
	}
	_ = backfillModules(profile, now)
}

func cloneProfileModules(profile Profile) Profile {
	profile.Currencies = cloneInt64Map(profile.Currencies)
	profile.Inventory = cloneInt64Map(profile.Inventory)
	profile.Quests = cloneStringMap(profile.Quests)
	if profile.Mailbox != nil {
		profile.Mailbox = append([]MailRecord(nil), profile.Mailbox...)
	}
	return profile
}

func backfillCurrency(profile *Profile, _ time.Time) bool {
	if profile == nil || profile.Currencies != nil {
		return false
	}
	profile.Currencies = defaultCurrencies()
	return true
}

func backfillInventory(profile *Profile, _ time.Time) bool {
	if profile == nil || profile.Inventory != nil {
		return false
	}
	profile.Inventory = defaultInventory()
	return true
}

func backfillProfileQuest(profile *Profile, _ time.Time) bool {
	if profile == nil || profile.Quests != nil {
		return false
	}
	profile.Quests = defaultQuests()
	return true
}

func backfillProfileMail(profile *Profile, now time.Time) bool {
	if profile == nil || profile.Mailbox != nil {
		return false
	}
	profile.Mailbox = defaultMailbox(now)
	return true
}

func defaultCurrencies() map[string]int64 {
	return map[string]int64{
		"gold":    0,
		"diamond": 0,
	}
}

func defaultInventory() map[string]int64 {
	return map[string]int64{
		"starter_sword": 1,
	}
}

func defaultQuests() map[string]string {
	return map[string]string{
		"main_001": "accepted",
	}
}

func defaultMailbox(now time.Time) []MailRecord {
	return []MailRecord{
		{
			ID:        "welcome",
			Title:     "Welcome",
			State:     "unread",
			CreatedAt: now,
		},
	}
}

func markMailRead(profile *Profile, mailID string) (bool, bool) {
	for i := range profile.Mailbox {
		if profile.Mailbox[i].ID == mailID {
			if profile.Mailbox[i].State == "read" {
				return true, false
			}
			profile.Mailbox[i].State = "read"
			return true, true
		}
	}
	return false, false
}

func unreadMailCount(profile Profile) int {
	count := 0
	for _, mail := range profile.Mailbox {
		if mail.State != "read" {
			count++
		}
	}
	return count
}

func cloneInt64Map(in map[string]int64) map[string]int64 {
	if in == nil {
		return nil
	}
	out := make(map[string]int64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
