package dnfbridge

import (
	"longheng.io/server/internal/modules/dnf/alignedcmd"
	"longheng.io/server/internal/modules/dnf/dnfenum"
	dnfpet "longheng.io/server/internal/modules/dnf/pet"
)

// currentPetPVFCatalog lazily loads the typed pet catalog from the same active
// runtime Script.pvf used by the other bridge-owned catalogs. Both success and
// failure are cached so a malformed/missing PVF cannot trigger repeated heavy
// archive scans or silently fall back to request/inventory metadata.
func (s *Service) currentPetPVFCatalog() (*dnfpet.PVFCatalog, error) {
	if s == nil {
		return nil, dnfpet.ErrPetPVFSourceRequired
	}
	s.petCatalogMu.Lock()
	defer s.petCatalogMu.Unlock()
	if s.petCatalog != nil || s.petCatalogLoadErr != nil {
		return s.petCatalog, s.petCatalogLoadErr
	}
	s.initialEquipmentMu.Lock()
	archive, err := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if err == nil {
		s.petCatalog, err = dnfpet.NewPVFCatalog(archive)
	}
	s.petCatalogLoadErr = err
	if err == nil && s.petCatalog != nil {
		s.logInfo("dnf pet PVF catalog loaded", "max_creature_level", dnfpet.MaxCreatureLevel)
	}
	return s.petCatalog, s.petCatalogLoadErr
}

// alignedPetHatchResolverForCommand keeps PVF loading request-driven. Only the
// real current hatch command needs this dependency; other aligned commands do
// not open or scan the pet catalog.
func (s *Service) alignedPetHatchResolverForCommand(opcode dnfenum.CmdPacket) (alignedcmd.PetHatchResolver, error) {
	if opcode != dnfenum.CmdPacketHatchCreature {
		return nil, nil
	}
	catalog, err := s.currentPetPVFCatalog()
	if err != nil {
		return nil, err
	}
	if catalog == nil {
		return nil, dnfpet.ErrPetPVFSourceRequired
	}
	return func(eggItemID int64) (alignedcmd.PetHatchResolution, error) {
		definition, err := catalog.ResolveHatch(eggItemID)
		if err != nil {
			return alignedcmd.PetHatchResolution{}, err
		}
		return alignedcmd.PetHatchResolution{
			EggItemID:      definition.EggItemID,
			HatchedItemID:  definition.HatchedItemID,
			EggPVFPath:     definition.EggPVFPath,
			HatchedPVFPath: definition.HatchedPVFPath,
			MinimumLevel:   definition.MinimumLevel,
		}, nil
	}, nil
}
