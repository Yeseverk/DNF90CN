package dnfbridge

import (
	"math"

	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
)

func (s *Service) sendCurrentCeraShopPurchaseFailure(session *gameSession) error {
	return s.sendGameUpperRawClass(
		session,
		uint16(dnfenum.CmdPacketBuyCerashopItem),
		buildCurrentCeraShopPurchaseFailureBody(),
		dnfproto.DefaultChannelClassification,
	)
}

func buildCurrentCeraShopPurchaseSuccessBody(commodityID uint32, bonuses ...currentCeraShopBonusItem) []byte {
	return buildCurrentCeraShopPurchaseSuccessBodyWithCount(commodityID, 0, bonuses...)
}

// buildCurrentCeraShopPurchaseSuccessBodyWithCount keeps the three trailing
// u32 fields zero.  2026-07-25 live evidence: writing the purchased quantity
// into the first trailing u32 hangs the result dialog (client left
// unclickable), so those fields are not the row counts.  Quantity display
// remains an open investigation; the zero shape is the proven safe one.
func buildCurrentCeraShopPurchaseSuccessBodyWithCount(commodityID uint32, count uint32, bonuses ...currentCeraShopBonusItem) []byte {
	_ = count
	var writer packetWriter
	writer.writeByte(1)
	writer.writeByte(0)
	writer.writeInt32(-1) // category=-1: current reader performs a safe full catalog lookup.
	writer.writeUint32(commodityID)
	writer.writeUint32(0)
	writer.writeUint32(0)
	writer.writeUint32(0)
	// Bonus display rows drive the dialog's right-side 商城购物奖励 panel
	// (one-plus-one IPG gifts, e.g. 充满爱慕的信 -> 泰迪礼盒 + 幸运魔锤).
	// Row layout u32 itemID + u32 count.
	writer.writeUint16(uint16(len(bonuses)))
	for _, bonus := range bonuses {
		writer.writeUint32(bonus.ItemID)
		writer.writeUint32(bonus.Count)
	}
	return writer.bytes()
}

func buildCurrentCeraShopPurchaseFailureBody() []byte {
	var writer packetWriter
	writer.writeByte(0)
	writer.writeByte(4)
	writer.writeInt32(-1)
	writer.writeInt32(-1)
	writer.writeUint32(0)
	writer.writeUint32(0)
	writer.writeUint32(0)
	return writer.bytes()
}

func buildCurrentCeraShopBalanceBody(cera int64) []byte {
	if cera < 0 {
		cera = 0
	}
	if cera > math.MaxInt32 {
		cera = math.MaxInt32
	}
	var writer packetWriter
	writer.writeByte(1)
	writer.writeInt32(int(cera))
	writer.writeUint32(0) // no token-Cera repository exists.
	writer.writeUint32(0) // no happy-token-Cera repository exists.
	return writer.bytes()
}
