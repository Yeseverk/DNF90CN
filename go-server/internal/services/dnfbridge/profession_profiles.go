package dnfbridge

import (
	"context"
	"fmt"

	dnfprofession "longheng.io/server/internal/modules/dnf/profession"
	dnfskill "longheng.io/server/internal/modules/dnf/skill"
)

func (s *Service) preloadProfessionProfiles(ctx context.Context) error {
	profiles, catalog, err := s.currentProfessionResources(ctx)
	if err != nil {
		return fmt.Errorf("preload profession profiles: %w", err)
	}
	snapshot := profiles.Snapshot()
	s.logPacketEvent("dnf-profession-profiles-loaded",
		"jobs", snapshot.Jobs,
		"initial_grants", snapshot.InitialGrants,
		"class_grants", snapshot.ClassGrants,
		"awakening_grants", snapshot.AwakeningGrants,
		"missing_skill_grants", snapshot.MissingSkills,
		"skill_catalog_jobs", catalog.Snapshot().Jobs,
		"source", "runtime_pvf_character_chr")
	return nil
}

func (s *Service) currentProfessionResources(ctx context.Context) (*dnfprofession.Profiles, *dnfskill.Table, error) {
	if s == nil {
		return nil, nil, dnfprofession.ErrSourceRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.initialEquipmentMu.Lock()
	archive, err := s.initialEquipmentArchiveLocked()
	s.initialEquipmentMu.Unlock()
	if err != nil {
		return nil, nil, err
	}
	catalog, err := s.loadSkillCatalog(ctx, archive)
	if err != nil {
		return nil, nil, err
	}

	s.professionMu.Lock()
	defer s.professionMu.Unlock()
	if s.professionProfiles != nil || s.professionProfilesErr != nil {
		return s.professionProfiles, catalog, s.professionProfilesErr
	}
	profiles, err := dnfprofession.LoadProfiles(ctx, archive, catalog)
	if err != nil {
		s.professionProfilesErr = err
		return nil, nil, err
	}
	s.professionProfiles = profiles
	return profiles, catalog, nil
}
