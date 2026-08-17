// Package characterdata owns protocol-independent character state assembly.
// Packet builders may consume an Aggregate, but wire layouts do not belong here.
package characterdata

import (
	"context"
	"errors"
	"fmt"
	"strings"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

var (
	ErrCharacterIDRequired   = errors.New("character id is required")
	ErrCharacterNotFound     = errors.New("character not found")
	ErrRepositoryUnavailable = errors.New("character data repository is unavailable")
)

// Parts selects the durable child records to load with a character.
type Parts uint32

const (
	PartInventory Parts = 1 << iota
	PartEquipment
	PartSkill
	PartQuest
	PartPet

	AllParts = PartInventory | PartEquipment | PartSkill | PartQuest | PartPet
)

// Presence distinguishes a missing child record from a persisted empty record.
type Presence struct {
	Inventory bool
	Equipment bool
	Skill     bool
	Quest     bool
	Pet       bool
}

// Aggregate is a detached snapshot of one character and its requested durable
// child records. Generic settings are intentionally excluded until their
// character scopes become typed repositories.
type Aggregate struct {
	Character dnfrepo.CharacterRecord
	Inventory dnfrepo.InventoryRecord
	Equipment dnfrepo.EquipmentRecord
	Skill     dnfrepo.SkillRecord
	Quest     dnfrepo.QuestRecord
	Pet       dnfrepo.PetRecord
	Presence  Presence
}

// Loader assembles character state through repository interfaces without
// depending on dnfbridge sessions or packet formats.
type Loader struct {
	repositories dnfrepo.Group
}

func NewLoader(repositories dnfrepo.Group) *Loader {
	return &Loader{repositories: repositories}
}

// Load reads the character base record and requested child records.
func (l *Loader) Load(ctx context.Context, characterID string, parts Parts) (Aggregate, bool, error) {
	characterID = strings.TrimSpace(characterID)
	if characterID == "" {
		return Aggregate{}, false, ErrCharacterIDRequired
	}
	if l == nil || l.repositories.Character == nil {
		return Aggregate{}, false, repositoryError("character")
	}
	character, found, err := l.repositories.Character.Load(ctxOrBackground(ctx), characterID)
	if err != nil || !found {
		return Aggregate{}, found, err
	}
	aggregate, err := l.Hydrate(ctx, character, parts)
	if err != nil {
		return Aggregate{}, false, err
	}
	return aggregate, true, nil
}

// Hydrate attaches requested child records to an already loaded character.
func (l *Loader) Hydrate(ctx context.Context, character dnfrepo.CharacterRecord, parts Parts) (Aggregate, error) {
	character = dnfrepo.CloneCharacter(character)
	characterID := strings.TrimSpace(character.CharacterID)
	if characterID == "" {
		return Aggregate{}, ErrCharacterIDRequired
	}
	if l == nil {
		return Aggregate{}, repositoryError("group")
	}
	ctx = ctxOrBackground(ctx)
	aggregate := Aggregate{Character: character}
	var err error

	if parts&PartInventory != 0 {
		if l.repositories.Inventory == nil {
			return Aggregate{}, repositoryError("inventory")
		}
		aggregate.Inventory, aggregate.Presence.Inventory, err = l.repositories.Inventory.Load(ctx, characterID)
		if err != nil {
			return Aggregate{}, fmt.Errorf("load character inventory: %w", err)
		}
		aggregate.Inventory = dnfrepo.CloneInventory(aggregate.Inventory)
		if !aggregate.Presence.Inventory {
			aggregate.Inventory.CharacterID = characterID
		}
	}
	if parts&PartEquipment != 0 {
		if l.repositories.Equipment == nil {
			return Aggregate{}, repositoryError("equipment")
		}
		aggregate.Equipment, aggregate.Presence.Equipment, err = l.repositories.Equipment.Load(ctx, characterID)
		if err != nil {
			return Aggregate{}, fmt.Errorf("load character equipment: %w", err)
		}
		aggregate.Equipment = dnfrepo.CloneEquipment(aggregate.Equipment)
		if !aggregate.Presence.Equipment {
			aggregate.Equipment.CharacterID = characterID
		}
	}
	if parts&PartSkill != 0 {
		if l.repositories.Skill == nil {
			return Aggregate{}, repositoryError("skill")
		}
		aggregate.Skill, aggregate.Presence.Skill, err = l.repositories.Skill.Load(ctx, characterID)
		if err != nil {
			return Aggregate{}, fmt.Errorf("load character skill: %w", err)
		}
		aggregate.Skill = dnfrepo.CloneSkill(aggregate.Skill)
		if !aggregate.Presence.Skill {
			aggregate.Skill.CharacterID = characterID
		}
	}
	if parts&PartQuest != 0 {
		if l.repositories.Quest == nil {
			return Aggregate{}, repositoryError("quest")
		}
		aggregate.Quest, aggregate.Presence.Quest, err = l.repositories.Quest.Load(ctx, characterID)
		if err != nil {
			return Aggregate{}, fmt.Errorf("load character quest: %w", err)
		}
		aggregate.Quest = dnfrepo.CloneQuest(aggregate.Quest)
		if !aggregate.Presence.Quest {
			aggregate.Quest.CharacterID = characterID
		}
	}
	if parts&PartPet != 0 {
		if l.repositories.Pet == nil {
			return Aggregate{}, repositoryError("pet")
		}
		aggregate.Pet, aggregate.Presence.Pet, err = l.repositories.Pet.Load(ctx, characterID)
		if err != nil {
			return Aggregate{}, fmt.Errorf("load character pet: %w", err)
		}
		aggregate.Pet = dnfrepo.ClonePet(aggregate.Pet)
		if !aggregate.Presence.Pet {
			aggregate.Pet.CharacterID = characterID
		}
	}
	return aggregate, nil
}

func repositoryError(name string) error {
	return fmt.Errorf("%w: %s", ErrRepositoryUnavailable, name)
}

func ctxOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
