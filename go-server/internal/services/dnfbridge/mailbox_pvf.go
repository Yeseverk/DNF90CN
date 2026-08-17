package dnfbridge

import (
	"fmt"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
)

func (s *Service) alignedMailboxItemResolverForCommand(opcode dnfenum.CmdPacket) (alignedcmd.MailboxItemResolver, error) {
	needsPrice := opcode == dnfenum.CmdPacketMailboxSend || opcode == dnfenum.CmdPacketMultiMailboxSend
	if !needsPrice && opcode != dnfenum.CmdPacketMailboxExtractItem {
		return nil, nil
	}
	catalog, err := s.currentPVFItemCatalog()
	if err != nil {
		return nil, err
	}
	return func(itemID uint32) (alignedcmd.MailboxItemResolution, error) {
		definition, resolveErr := catalog.ResolveItem(itemID)
		if resolveErr != nil {
			return alignedcmd.MailboxItemResolution{}, resolveErr
		}
		return mailboxItemResolutionFromPVF(definition, needsPrice)
	}, nil
}

func mailboxItemResolutionFromPVF(definition dungeonDropItemDefinition, needsPrice bool) (alignedcmd.MailboxItemResolution, error) {
	// A listed, successfully parsed PVF item remains authoritative even when
	// its document omits [price]. The current composer accepts those items and
	// displays only the fixed attachment postage, so an absent price contributes
	// zero value instead of invalidating the durable attachment.
	if needsPrice && definition.Price < 0 {
		return alignedcmd.MailboxItemResolution{}, fmt.Errorf("mailbox item %d has invalid negative runtime-PVF price", definition.ItemID)
	}
	stackLimit := definition.StackLimit
	if definition.Kind == dungeonDropItemEquipment {
		stackLimit = 1
	}
	return alignedcmd.MailboxItemResolution{
		Price:         definition.Price,
		PVFPath:       definition.PVFPath,
		Kind:          string(definition.Kind),
		EquipmentType: definition.EquipmentType,
		SlotStart:     definition.SlotStart,
		SlotEnd:       definition.SlotEnd,
		StackLimit:    stackLimit,
	}, nil
}
