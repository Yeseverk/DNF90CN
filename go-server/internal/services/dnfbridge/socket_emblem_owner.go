package dnfbridge

import (
	"strconv"

	dnfsocketemblem "longheng.io/server/internal/modules/dnf/socketemblem"
)

func (s *Service) currentSocketMutationOwner(session *gameSession) (string, *dnfsocketemblem.Owner, *pvfDungeonDropCatalog, error) {
	if session == nil || session.selectedCharacterID == 0 {
		return "", nil, nil, errCurrentSocketSelectedCharacterMissing
	}
	repositories, ok := s.repositoryGroup()
	if !ok || repositories.Inventory == nil || repositories.Equipment == nil {
		return "", nil, nil, errCurrentSocketRepositoryMissing
	}
	if repositories.CharacterItems == nil {
		return "", nil, nil, errCurrentSocketTransactionMissing
	}
	owner, err := dnfsocketemblem.NewOwner(repositories)
	if err != nil {
		return "", nil, nil, errCurrentSocketTransactionMissing
	}
	catalog, err := s.currentPVFItemCatalog()
	if err != nil {
		return "", nil, nil, err
	}
	return strconv.Itoa(int(session.selectedCharacterID)), owner, catalog, nil
}
