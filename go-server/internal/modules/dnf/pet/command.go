package pet

import (
	"fmt"
	"strings"

	"longheng.io/server/internal/modules/dnf/alignedcmd"
)

// Command records decoded intent only. All authoritative item and creature
// data is loaded by the owner from DB/PVF state.
type Command struct {
	Operation           string
	AccountID           string
	SelectedCharacterID uint16
	ListType            byte
	SlotIndex           int16
	NameLength          int
	RawLen              int
	NeedsOwner          string
}

func NewRenameCommand(req alignedcmd.Request, parsed RenameCreatureRequest) Command {
	return Command{
		Operation:           "rename_creature",
		AccountID:           strings.TrimSpace(req.AccountID),
		SelectedCharacterID: req.SelectedCharacterID,
		ListType:            parsed.ListType,
		SlotIndex:           parsed.SlotIndex,
		NameLength:          len(parsed.NameRaw),
		NeedsOwner:          "inventory/PetRecord serial-item-owner validation + character pet transaction + current EXE op100 ACK",
	}
}

func NewHatchCommand(req alignedcmd.Request, operation string, parsed HatchCreatureRequest) Command {
	return Command{
		Operation:           operation,
		AccountID:           strings.TrimSpace(req.AccountID),
		SelectedCharacterID: req.SelectedCharacterID,
		ListType:            parsed.ListType,
		SlotIndex:           parsed.SlotIndex,
		NeedsOwner:          "runtime PVF pet catalog + inventory/equipment/pet transaction + current EXE refresh",
	}
}

func NewRequestCommand(req alignedcmd.Request) Command {
	return Command{
		Operation:           "request_hatched_creature",
		AccountID:           strings.TrimSpace(req.AccountID),
		SelectedCharacterID: req.SelectedCharacterID,
		RawLen:              len(req.Body),
		NeedsOwner:          "typed pet owner read model + current EXE op105 encoder",
	}
}

func (c Command) String() string {
	base := fmt.Sprintf("account=%q char=%d", c.AccountID, c.SelectedCharacterID)
	if c.Operation == "request_hatched_creature" {
		return fmt.Sprintf("%s rawLen=%d needs=%s", base, c.RawLen, c.NeedsOwner)
	}
	if c.Operation == "rename_creature" {
		return fmt.Sprintf("%s list=%d slot=%d nameBytes=%d needs=%s", base, c.ListType, c.SlotIndex, c.NameLength, c.NeedsOwner)
	}
	return fmt.Sprintf("%s list=%d slot=%d needs=%s", base, c.ListType, c.SlotIndex, c.NeedsOwner)
}
