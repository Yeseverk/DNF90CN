package dnfbridge

import "testing"

func TestMailboxItemResolutionAllowsListedPVFItemWithoutPrice(t *testing.T) {
	got, err := mailboxItemResolutionFromPVF(dungeonDropItemDefinition{
		ItemID:     3166,
		Kind:       dungeonDropItemStackable,
		PVFPath:    "stackable/reproduced-item.stk",
		PriceFound: false,
		SlotStart:  121,
		SlotEnd:    176,
		StackLimit: 1000,
	}, true)
	if err != nil {
		t.Fatalf("mailboxItemResolutionFromPVF error = %v", err)
	}
	if got.Price != 0 || got.PVFPath != "stackable/reproduced-item.stk" ||
		got.SlotStart != 121 || got.SlotEnd != 176 || got.StackLimit != 1000 {
		t.Fatalf("mailbox item resolution = %+v, want listed zero-value stackable", got)
	}
}

func TestMailboxItemResolutionRejectsNegativePrice(t *testing.T) {
	_, err := mailboxItemResolutionFromPVF(dungeonDropItemDefinition{
		ItemID:     3166,
		Kind:       dungeonDropItemStackable,
		Price:      -1,
		PriceFound: true,
	}, true)
	if err == nil {
		t.Fatal("negative mailbox PVF price should be rejected")
	}
}
