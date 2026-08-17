// Package itemtrade owns the durable, two-character inventory exchange used
// by the current client's town item-trade flow. Wire/session state remains in
// dnfbridge; this package validates offers and commits both inventories.
package itemtrade

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var (
	ErrRepositoryUnavailable = errors.New("item trade repository is unavailable")
	ErrInvalidParticipants   = errors.New("item trade participants are invalid")
	ErrInvalidOffer          = errors.New("item trade offer is invalid")
	ErrItemUnavailable       = errors.New("item trade item is unavailable")
	ErrItemNotTradeable      = errors.New("item is not tradeable")
	ErrInventoryFull         = errors.New("item trade destination inventory is full")
	ErrGoldUnavailable       = errors.New("item trade gold is unavailable")
	ErrGoldOverflow          = errors.New("item trade gold would overflow")
)

const (
	mainListType   byte = 0
	avatarListType byte = 1

	equipmentFirst  int16 = 9
	equipmentLast   int16 = 64
	consumableFirst int16 = 65
	consumableLast  int16 = 120
	materialFirst   int16 = 121
	materialLast    int16 = 176
	questFirst      int16 = 177
	questLast       int16 = 232
	professionFirst int16 = 233
	professionLast  int16 = 288
	emblemFirst     int16 = 289
	emblemLast      int16 = 344
	specialFirst    int16 = 345
	specialLast     int16 = 353

	avatarFirst int16 = 0
	avatarLast  int16 = 500
)

type Offer struct {
	TradeSlot    uint16
	SourceList   byte
	SourceSlot   int16
	Count        int64
	ExpectedItem int64
}

type Participant struct {
	CharacterID string
	Offers      []Offer
	Gold        int64
}

type Transfer struct {
	FromCharacterID string
	ToCharacterID   string
	TradeSlot       uint16
	DestinationList byte
	DestinationSlot int16
	Stack           dnfrepo.ItemStack
}

type Result struct {
	Inventories map[string]dnfrepo.InventoryRecord
	Characters  map[string]dnfrepo.CharacterRecord
	Received    map[string][]Transfer
}

type Owner struct {
	repositories dnfrepo.Group
	now          func() time.Time
}

func NewOwner(repositories dnfrepo.Group) (*Owner, error) {
	if repositories.CharacterTrade == nil {
		return nil, ErrRepositoryUnavailable
	}
	return &Owner{repositories: repositories, now: time.Now}, nil
}

func (o *Owner) Exchange(ctx context.Context, first Participant, second Participant) (Result, error) {
	if o == nil || o.repositories.CharacterTrade == nil {
		return Result{}, ErrRepositoryUnavailable
	}
	first.CharacterID = strings.TrimSpace(first.CharacterID)
	second.CharacterID = strings.TrimSpace(second.CharacterID)
	if first.CharacterID == "" || second.CharacterID == "" || first.CharacterID == second.CharacterID {
		return Result{}, ErrInvalidParticipants
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	participants := []Participant{first, second}
	if first.Gold < 0 || second.Gold < 0 {
		return Result{}, ErrInvalidOffer
	}
	sort.Slice(participants, func(i, j int) bool {
		return participants[i].CharacterID < participants[j].CharacterID
	})
	result := Result{}
	err := o.repositories.CharacterTrade.WithinCharacterTrade(
		ctx,
		participants[0].CharacterID,
		participants[1].CharacterID,
		func(characters dnfrepo.CharacterRepository, inventories dnfrepo.InventoryRepository) error {
			loaded := make(map[string]dnfrepo.InventoryRecord, 2)
			for _, participant := range participants {
				record, found, err := inventories.Load(ctx, participant.CharacterID)
				if err != nil {
					return err
				}
				if !found {
					record = dnfrepo.InventoryRecord{CharacterID: participant.CharacterID}
				}
				if record.Slots == nil {
					record.Slots = make(map[string]dnfrepo.ItemStack)
				}
				loaded[participant.CharacterID] = record
			}
			loadedCharacters := make(map[string]dnfrepo.CharacterRecord, 2)
			if participants[0].Gold > 0 || participants[1].Gold > 0 {
				for _, participant := range participants {
					record, found, err := characters.Load(ctx, participant.CharacterID)
					if err != nil {
						return err
					}
					if !found {
						return fmt.Errorf("character %s: %w", participant.CharacterID, ErrGoldUnavailable)
					}
					if record.Stats == nil {
						record.Stats = make(map[string]int64)
					}
					loadedCharacters[participant.CharacterID] = record
				}
				for index, participant := range participants {
					recipient := participants[1-index]
					record := loadedCharacters[participant.CharacterID]
					balance := record.Stats["gold"]
					if balance < participant.Gold {
						return fmt.Errorf("character %s balance=%d offer=%d: %w", participant.CharacterID, balance, participant.Gold, ErrGoldUnavailable)
					}
					balance -= participant.Gold
					if recipient.Gold > math.MaxInt64-balance {
						return fmt.Errorf("character %s balance=%d incoming=%d: %w", participant.CharacterID, balance, recipient.Gold, ErrGoldOverflow)
					}
					record.Stats["gold"] = balance + recipient.Gold
					loadedCharacters[participant.CharacterID] = record
				}
			}

			staged := make(map[string][]stagedOffer, 2)
			for _, participant := range participants {
				rows, err := validateOffers(loaded[participant.CharacterID], participant.Offers, o.clock())
				if err != nil {
					return fmt.Errorf("character %s: %w", participant.CharacterID, err)
				}
				staged[participant.CharacterID] = rows
			}

			// Remove every outgoing quantity before assigning incoming slots. This
			// lets a full inventory complete a one-for-one exchange without a
			// transient capacity failure.
			for _, participant := range participants {
				record := loaded[participant.CharacterID]
				for _, row := range staged[participant.CharacterID] {
					stack := record.Slots[row.sourceKey]
					if row.offer.Count == stack.Count {
						delete(record.Slots, row.sourceKey)
					} else {
						stack.Count -= row.offer.Count
						record.Slots[row.sourceKey] = stack
					}
				}
				loaded[participant.CharacterID] = record
			}

			received := make(map[string][]Transfer, 2)
			for index, participant := range participants {
				recipient := participants[1-index]
				record := loaded[recipient.CharacterID]
				for _, row := range staged[participant.CharacterID] {
					destinationSlot, err := allocateDestinationSlot(record.Slots, row.offer.SourceList, row.offer.SourceSlot)
					if err != nil {
						return fmt.Errorf("character %s receiving trade slot %d: %w", recipient.CharacterID, row.offer.TradeSlot, err)
					}
					stack := cloneStack(row.stack)
					stack.Count = row.offer.Count
					destinationKey := slotKey(row.offer.SourceList, destinationSlot)
					record.Slots[destinationKey] = stack
					received[recipient.CharacterID] = append(received[recipient.CharacterID], Transfer{
						FromCharacterID: participant.CharacterID,
						ToCharacterID:   recipient.CharacterID,
						TradeSlot:       row.offer.TradeSlot,
						DestinationList: row.offer.SourceList,
						DestinationSlot: destinationSlot,
						Stack:           cloneStack(stack),
					})
				}
				loaded[recipient.CharacterID] = record
			}

			for _, participant := range participants {
				record := loaded[participant.CharacterID]
				record.UpdatedAt = o.clock().UTC()
				if err := inventories.Save(ctx, record); err != nil {
					return err
				}
				loaded[participant.CharacterID] = record
			}
			for _, participant := range participants {
				record, changed := loadedCharacters[participant.CharacterID]
				if !changed {
					continue
				}
				record.UpdatedAt = o.clock().UTC()
				if err := characters.Save(ctx, record); err != nil {
					return err
				}
				loadedCharacters[participant.CharacterID] = record
			}
			result = Result{Inventories: loaded, Characters: loadedCharacters, Received: received}
			return nil
		},
	)
	if err != nil {
		return Result{}, err
	}
	return result, nil
}

type stagedOffer struct {
	offer     Offer
	sourceKey string
	stack     dnfrepo.ItemStack
}

func validateOffers(record dnfrepo.InventoryRecord, offers []Offer, now time.Time) ([]stagedOffer, error) {
	rows := make([]stagedOffer, 0, len(offers))
	tradeSlots := make(map[uint16]struct{}, len(offers))
	sourceCounts := make(map[string]int64, len(offers))
	ordered := append([]Offer(nil), offers...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].TradeSlot < ordered[j].TradeSlot })
	for _, offer := range ordered {
		if _, duplicate := tradeSlots[offer.TradeSlot]; duplicate || offer.Count <= 0 || !supportedSourceList(offer.SourceList) || offer.SourceSlot < 0 {
			return nil, ErrInvalidOffer
		}
		if dnfrepo.IsAccountSharedInventorySlot(offer.SourceList, offer.SourceSlot) {
			return nil, ErrItemNotTradeable
		}
		tradeSlots[offer.TradeSlot] = struct{}{}
		key := slotKey(offer.SourceList, offer.SourceSlot)
		stack, found := record.Slots[key]
		if !found || stack.ItemID <= 0 || stack.Count <= 0 || (offer.ExpectedItem > 0 && stack.ItemID != offer.ExpectedItem) {
			return nil, ErrItemUnavailable
		}
		if stack.Bind || (!stack.ExpireAt.IsZero() && !stack.ExpireAt.After(now)) {
			return nil, ErrItemNotTradeable
		}
		sourceCounts[key] += offer.Count
		if sourceCounts[key] > stack.Count {
			return nil, ErrItemUnavailable
		}
		rows = append(rows, stagedOffer{offer: offer, sourceKey: key, stack: cloneStack(stack)})
	}
	return rows, nil
}

func allocateDestinationSlot(slots map[string]dnfrepo.ItemStack, listType byte, preferred int16) (int16, error) {
	if !supportedSourceList(listType) {
		return 0, ErrInvalidOffer
	}
	start, end, ok := destinationSlotRange(listType, preferred)
	if !ok {
		return 0, ErrInvalidOffer
	}
	for slot := start; slot <= end; slot++ {
		if _, occupied := slots[slotKey(listType, slot)]; !occupied {
			return slot, nil
		}
	}
	return 0, ErrInventoryFull
}

// destinationSlotRange keeps an incoming item in the same current-client bag
// page as its source. The source index is not a portable destination: copying
// a sender's expanded high slot into a recipient with only the base capacity
// persists the item but leaves it outside the recipient's visible cells.
func destinationSlotRange(listType byte, sourceSlot int16) (int16, int16, bool) {
	if listType == avatarListType {
		if sourceSlot < avatarFirst || sourceSlot > avatarLast {
			return 0, 0, false
		}
		return avatarFirst, avatarLast, true
	}
	for _, page := range [...]struct{ first, last int16 }{
		{equipmentFirst, equipmentLast},
		{consumableFirst, consumableLast},
		{materialFirst, materialLast},
		{questFirst, questLast},
		{professionFirst, professionLast},
		{emblemFirst, emblemLast},
		{specialFirst, specialLast},
	} {
		if sourceSlot >= page.first && sourceSlot <= page.last {
			return page.first, page.last, true
		}
	}
	return 0, 0, false
}

func supportedSourceList(listType byte) bool {
	return listType == mainListType || listType == avatarListType
}

func validDestinationSlot(listType byte, slot int16) bool {
	_, _, ok := destinationSlotRange(listType, slot)
	return ok
}

func slotKey(listType byte, slot int16) string {
	return strconv.Itoa(int(listType)) + ":" + strconv.FormatInt(int64(slot), 10)
}

func cloneStack(stack dnfrepo.ItemStack) dnfrepo.ItemStack {
	stack.RawEntry = append([]byte(nil), stack.RawEntry...)
	if stack.Extra != nil {
		extra := make(map[string]string, len(stack.Extra))
		for key, value := range stack.Extra {
			extra[key] = value
		}
		stack.Extra = extra
	}
	return stack
}

func (o *Owner) clock() time.Time {
	if o != nil && o.now != nil {
		return o.now()
	}
	return time.Now()
}
