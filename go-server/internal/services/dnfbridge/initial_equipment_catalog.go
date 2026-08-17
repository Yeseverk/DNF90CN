package dnfbridge

import (
	"context"
	"fmt"
	"path"
	"strings"

	platformpvf "longheng.io/server/internal/platform/pvf"
)

const (
	initialEquipmentCharacterList = "character/character.lst"
	initialEquipmentItemList      = "equipment/equipment.lst"
	initialEquipmentCreateValue   = 1
	initialEquipmentDefaultDur    = 45
)

type initialEquipmentTextSource interface {
	ReadText(relativePath string) (string, error)
}

type initialEquipmentEntry struct {
	Slot        int16
	ItemID      int64
	Durability  uint16
	EquipType   int64
	PVFPath     string
	RawEntry    []byte
	ModelLayers []initialEquipmentModelLayer
}

type initialEquipmentModelLayer struct {
	Key    uint16
	Name   string
	Script string
}

func (s *Service) initialCharacterEquipment(ctx context.Context, job byte) ([]initialEquipmentEntry, error) {
	if s == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.initialEquipmentMu.Lock()
	defer s.initialEquipmentMu.Unlock()
	if s.initialEquipmentByJob == nil {
		s.initialEquipmentByJob = make(map[byte][]initialEquipmentEntry)
	}
	if cached, ok := s.initialEquipmentByJob[job]; ok {
		return cloneInitialEquipmentEntries(cached), nil
	}
	archive, err := s.initialEquipmentArchiveLocked()
	if err != nil {
		return nil, err
	}
	entries, err := parseInitialCharacterEquipmentFromSource(archive, job)
	if err != nil {
		return nil, err
	}
	s.initialEquipmentByJob[job] = cloneInitialEquipmentEntries(entries)
	return cloneInitialEquipmentEntries(entries), nil
}

func (s *Service) preloadInitialEquipmentIndex(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.initialEquipmentMu.Lock()
	archive, err := s.initialEquipmentArchiveLocked()
	if err != nil {
		s.initialEquipmentMu.Unlock()
		return fmt.Errorf("preload initial equipment pvf: %w", err)
	}
	byJob, err := parseInitialCharacterEquipmentAllFromSource(archive)
	if err != nil {
		s.initialEquipmentMu.Unlock()
		return fmt.Errorf("preload initial equipment index: %w", err)
	}
	s.initialEquipmentByJob = byJob
	jobs := len(byJob)
	entries := 0
	for _, rows := range byJob {
		entries += len(rows)
	}
	s.initialEquipmentMu.Unlock()
	s.logPacketEvent("dnf-initial-equipment-index-loaded", "jobs", jobs, "entries", entries)
	return nil
}

func (s *Service) initialEquipmentArchiveLocked() (*platformpvf.Archive, error) {
	if s.initialEquipmentArchive != nil {
		return s.initialEquipmentArchive, nil
	}
	if s.initialEquipmentLoadErr != nil {
		return nil, s.initialEquipmentLoadErr
	}
	pvfPath := strings.TrimSpace(s.options.pvfPath)
	if pvfPath == "" {
		pvfPath = defaultPVFPath
	}
	maxBytes := s.options.pvfMaxBytes
	if maxBytes <= 0 {
		maxBytes = platformpvf.DefaultMaxBytes
	}
	archive, err := platformpvf.LoadArchive(platformpvf.Options{Path: pvfPath, MaxBytes: maxBytes})
	if err != nil {
		s.initialEquipmentLoadErr = err
		return nil, err
	}
	s.initialEquipmentArchive = archive
	if snapshot := archive.Snapshot(); snapshot.FileCount > 0 {
		s.logPacketEvent("dnf-initial-equipment-pvf-loaded",
			"path", snapshot.Path,
			"format", string(snapshot.Format),
			"files", snapshot.FileCount)
	}
	return archive, nil
}

func readInitialPVFText(source initialEquipmentTextSource, candidates ...string) (string, string, error) {
	seen := make(map[string]struct{}, len(candidates))
	var lastErr error
	for _, candidate := range candidates {
		clean := cleanInitialPVFPath(candidate)
		if clean == "" {
			continue
		}
		key := strings.ToLower(clean)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		text, err := source.ReadText(clean)
		if err == nil {
			return text, clean, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return "", "", lastErr
	}
	return "", "", fmt.Errorf("pvf path candidates are empty")
}

func initialPVFPath(base, ref string) string {
	base = cleanInitialPVFPath(base)
	ref = cleanInitialPVFPath(ref)
	if ref == "" || base == "" {
		return ref
	}
	lowerRef := strings.ToLower(ref)
	lowerBase := strings.ToLower(base)
	if lowerRef == lowerBase || strings.HasPrefix(lowerRef, lowerBase+"/") {
		return ref
	}
	return cleanInitialPVFPath(base + "/" + ref)
}

func cleanInitialPVFPath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	for strings.HasPrefix(value, "./") || strings.HasPrefix(value, "/") {
		value = strings.TrimPrefix(value, "./")
		value = strings.TrimPrefix(value, "/")
	}
	if value == "" {
		return ""
	}
	clean := strings.TrimSuffix(path.Clean(value), "/")
	if clean == "." {
		return ""
	}
	return clean
}

func normalizeInitialEquipmentSlotName(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func cloneInitialEquipmentEntries(entries []initialEquipmentEntry) []initialEquipmentEntry {
	if len(entries) == 0 {
		return nil
	}
	out := make([]initialEquipmentEntry, len(entries))
	for idx, entry := range entries {
		entry.RawEntry = append([]byte(nil), entry.RawEntry...)
		entry.ModelLayers = append([]initialEquipmentModelLayer(nil), entry.ModelLayers...)
		out[idx] = entry
	}
	return out
}
