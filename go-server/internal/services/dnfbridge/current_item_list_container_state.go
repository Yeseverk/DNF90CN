package dnfbridge

import (
	"context"

	dnfinventory "longheng.io/server/internal/modules/dnf/inventory"
	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func (s *Service) loadCurrentItemListContainerState(
	ctx context.Context,
	repository dnfrepo.SettingsRepository,
	characterID string,
	listType byte,
) dnfrepo.CharacterContainerState {
	switch listType {
	case 0, 1, 2:
	default:
		return dnfrepo.CharacterContainerState{}
	}
	owner, err := dnfinventory.NewContainerStateOwner(repository)
	if err != nil {
		s.logPacketEvent("game-upper-current-item-list-container-state-missing",
			"char_id", characterID,
			"list_type", listType,
			"fallback", "settings_repository_unavailable")
		return dnfrepo.CharacterContainerState{}
	}
	result, err := owner.Ensure(ctx, dnfinventory.EnsureContainerStateCommand{
		CharacterID: characterID,
		Initial:     newCharacterContainerStateSettings,
	})
	if err != nil {
		s.logPacketEvent("game-upper-current-item-list-container-state-invalid",
			"char_id", characterID,
			"list_type", listType,
			"err", err)
		return dnfrepo.CharacterContainerState{}
	}
	state := result.State
	if result.Created {
		s.logPacketEvent("game-upper-current-item-list-container-state-initialized",
			"char_id", characterID,
			"list_type", listType,
			"main_slot_count", state.MainSlotCount,
			"avatar_expansion", state.AvatarExpansion,
			"personal_cargo_slot_count", state.PersonalCargoSlotCount)
		return state
	}
	s.logPacketEvent("game-upper-current-item-list-container-state-loaded",
		"char_id", characterID,
		"list_type", listType,
		"main_slot_count", state.MainSlotCount,
		"avatar_expansion", state.AvatarExpansion,
		"personal_cargo_slot_count", state.PersonalCargoSlotCount,
		"account_cargo_selection_key", state.AccountCargoSelectionKey,
		"account_cargo_state_value", state.AccountCargoStateValue,
		"state_source", state.Source)
	return state
}
