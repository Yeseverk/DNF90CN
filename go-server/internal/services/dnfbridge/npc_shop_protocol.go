package dnfbridge

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

const (
	currentNPCShopBuyRequestWireSize           = 16
	currentNPCShopSellRequestWireSize          = 11
	currentNPCShopPurchaseCountRequestWireSize = 4
	currentNPCShopPurchaseCountListCount       = 4
	currentNPCShopBuyErrorCode                 = byte(0x04)
	currentNPCShopSellErrorCode                = byte(0x11)
	currentNPCShopMaxBuyCount                  = uint32(math.MaxUint16)
)

var errCurrentNPCShopRequestInvalid = errors.New("dnf current NPC shop request is invalid")

type currentNPCShopBuyRequest struct {
	ItemID uint32
	// Count is the raw wire value until PVF item-kind validation. The current
	// client sends 0 for one equipment item and positive counts for stackables.
	Count        uint32
	ShopContext  uint32
	ActorContext uint32
}

type currentNPCShopSellRequest struct {
	ListType     byte
	Slot         int16
	Count        uint32
	ActorContext uint32
}

type currentNPCShopBuyResult struct {
	GoldAfter        int64
	SPAfter          int64
	CoinAfter        int64
	Item             currentItemListEntry
	Updates          []currentItemListEntry
	Slot             int16
	ItemID           uint32
	Count            uint32
	CostItemID       uint32
	CostItemNewCount int64
}

type currentNPCShopSellResult struct {
	GoldAfter int64
	ListType  byte
	Slot      int16
	Applied   uint32
	ItemID    uint32
	Updates   []currentItemListEntry
}

func parseCurrentNPCShopPurchaseCountRequest(body []byte) (uint32, error) {
	if len(body) == 0 {
		return 0, nil
	}
	if len(body) != currentNPCShopPurchaseCountRequestWireSize {
		return 0, fmt.Errorf("%w: purchase-count body length=%d want=0 or %d", errCurrentNPCShopRequestInvalid, len(body), currentNPCShopPurchaseCountRequestWireSize)
	}
	return binary.LittleEndian.Uint32(body), nil
}

func parseCurrentNPCShopBuyRequest(body []byte) (currentNPCShopBuyRequest, error) {
	if len(body) != currentNPCShopBuyRequestWireSize {
		return currentNPCShopBuyRequest{}, fmt.Errorf("%w: buy body length=%d want=%d", errCurrentNPCShopRequestInvalid, len(body), currentNPCShopBuyRequestWireSize)
	}
	request := currentNPCShopBuyRequest{
		ItemID:       binary.LittleEndian.Uint32(body[0:4]),
		Count:        binary.LittleEndian.Uint32(body[4:8]),
		ShopContext:  binary.LittleEndian.Uint32(body[8:12]),
		ActorContext: binary.LittleEndian.Uint32(body[12:16]),
	}
	if request.ItemID == 0 || request.Count > currentNPCShopMaxBuyCount {
		return currentNPCShopBuyRequest{}, fmt.Errorf("%w: buy item=%d count=%d", errCurrentNPCShopRequestInvalid, request.ItemID, request.Count)
	}
	return request, nil
}

func normalizeCurrentNPCShopBuyCount(definition dungeonDropItemDefinition, wireCount uint32) (uint32, error) {
	switch definition.Kind {
	case dungeonDropItemEquipment:
		if wireCount > 1 {
			return 0, fmt.Errorf("%w: equipment item=%d wire_count=%d", errCurrentNPCShopProductUnavailable, definition.ItemID, wireCount)
		}
		return 1, nil
	case dungeonDropItemStackable:
		if wireCount == 0 {
			return 0, fmt.Errorf("%w: stackable item=%d wire_count=0", errCurrentNPCShopRequestInvalid, definition.ItemID)
		}
		return wireCount, nil
	default:
		return 0, fmt.Errorf("%w: item=%d kind=%s", errCurrentNPCShopProductUnavailable, definition.ItemID, definition.Kind)
	}
}

func parseCurrentNPCShopSellRequest(body []byte) (currentNPCShopSellRequest, error) {
	if len(body) != currentNPCShopSellRequestWireSize {
		return currentNPCShopSellRequest{}, fmt.Errorf("%w: sell body length=%d want=%d", errCurrentNPCShopRequestInvalid, len(body), currentNPCShopSellRequestWireSize)
	}
	slot := int16(binary.LittleEndian.Uint16(body[1:3]))
	request := currentNPCShopSellRequest{
		ListType:     body[0],
		Slot:         slot,
		Count:        binary.LittleEndian.Uint32(body[3:7]),
		ActorContext: binary.LittleEndian.Uint32(body[7:11]),
	}
	if request.ListType != dnfrepo.MainInventoryListType || request.Slot <= 0 || request.Count == 0 ||
		dnfrepo.IsAccountSharedInventorySlot(request.ListType, request.Slot) {
		return currentNPCShopSellRequest{}, fmt.Errorf("%w: sell list=%d slot=%d count=%d", errCurrentNPCShopRequestInvalid, request.ListType, request.Slot, request.Count)
	}
	return request, nil
}

func buildCurrentNPCShopBuySuccessBody(result currentNPCShopBuyResult) []byte {
	var body packetWriter
	body.writeByte(1)
	body.writeUint32(currentNPCShopWalletUint32(result.GoldAfter))
	body.writeUint32(currentNPCShopWalletUint32(result.SPAfter))
	body.writeUint32(0)
	body.writeUint32(currentNPCShopWalletUint32(result.CoinAfter))
	body.writeBytes(result.Item.data[:])
	// 86JP BuyItemAckBuilder: trailing cost-item rows (u8 count, then
	// u32 item id + u32 new stack count each). Gold buys carry none; material
	// exchanges report the consumed material's remaining count.
	if result.CostItemID == 0 {
		body.writeByte(0)
	} else {
		body.writeByte(1)
		body.writeUint32(result.CostItemID)
		body.writeUint32(currentNPCShopWalletUint32(result.CostItemNewCount))
	}
	return body.bytes()
}

func buildCurrentNPCShopPurchaseCountBody() []byte {
	var body packetWriter
	body.writeByte(1)
	for range currentNPCShopPurchaseCountListCount {
		body.writeUint32(0)
	}
	return body.bytes()
}

func buildCurrentNPCShopSellSuccessBody(result currentNPCShopSellResult) []byte {
	var body packetWriter
	body.writeByte(1)
	body.writeUint32(currentNPCShopWalletUint32(result.GoldAfter))
	body.writeByte(result.ListType)
	body.writeUint16(uint16(result.Slot))
	body.writeUint32(result.Applied)
	return body.bytes()
}

func buildCurrentNPCShopFailureBody(errorCode byte) []byte {
	return []byte{0, errorCode}
}

func currentNPCShopWalletUint32(value int64) uint32 {
	if value <= 0 {
		return 0
	}
	if value > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(value)
}
