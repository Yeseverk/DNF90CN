package joust

import (
	"context"
	"errors"
	"fmt"
	"strings"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

const EventPVFPath = "event/chn_event/chn_joust.evt"

var ErrPVFMalformed = errors.New("joust PVF is malformed")

// Rider is one immutable knight definition copied from the active Script.pvf.
// Win and Loss retain the four seven-value client battle timeline profiles;
// they are not the historical win/loss counters in op1241.
type Rider struct {
	ID         byte
	AttackType byte
	Name       string
	Win        []uint16
	Loss       []uint16
}

type Catalog struct {
	minimumLevel        int
	maximumBet          uint32
	rewardItemID        int64
	participationItemID int64
	materialItemIDs     []int64
	riders              []Rider
}

func LoadCatalog(ctx context.Context, source dnfpvf.Source) (*Catalog, error) {
	if source == nil {
		return nil, dnfpvf.ErrSourceRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	text, err := source.ReadText(EventPVFPath)
	if err != nil {
		return nil, fmt.Errorf("read joust PVF %q: %w", EventPVFPath, err)
	}
	document, err := dnfpvf.Parse(EventPVFPath, text)
	if err != nil {
		return nil, fmt.Errorf("parse joust PVF %q: %w", EventPVFPath, err)
	}

	minimumLevel, err := joustSingletonInt(document, "min level")
	if err != nil {
		return nil, err
	}
	maximumBet, err := joustSingletonInt(document, "max betting")
	if err != nil {
		return nil, err
	}
	rewardItemID, err := joustSingletonInt(document, "reward")
	if err != nil {
		return nil, err
	}
	participationItemID, err := joustSingletonInt(document, "betting reward")
	if err != nil {
		return nil, err
	}
	materials, err := joustIntegerSection(document, "material")
	if err != nil {
		return nil, err
	}
	riders, err := joustRiders(document)
	if err != nil {
		return nil, err
	}
	if minimumLevel != MinimumLevel || maximumBet != MaximumBetPerRound ||
		rewardItemID != PermanentCrystalID || participationItemID != ParticipationItemID ||
		len(materials) != 2 || materials[0] != PermanentCrystalID || materials[1] != EventCrystalID {
		return nil, fmt.Errorf(
			"%w: incompatible rules min=%d max=%d reward=%d betting_reward=%d materials=%v",
			ErrPVFMalformed,
			minimumLevel,
			maximumBet,
			rewardItemID,
			participationItemID,
			materials,
		)
	}
	if len(riders) < OpeningKnightCount {
		return nil, fmt.Errorf("%w: riders=%d want-at-least=%d", ErrPVFMalformed, len(riders), OpeningKnightCount)
	}
	return &Catalog{
		minimumLevel:        int(minimumLevel),
		maximumBet:          uint32(maximumBet),
		rewardItemID:        rewardItemID,
		participationItemID: participationItemID,
		materialItemIDs:     append([]int64(nil), materials...),
		riders:              cloneRiders(riders),
	}, nil
}

func (c *Catalog) Riders() []Rider {
	if c == nil {
		return nil
	}
	return cloneRiders(c.riders)
}

func joustSingletonInt(document *dnfpvf.Document, name string) (int64, error) {
	values, err := joustIntegerSection(document, name)
	if err != nil {
		return 0, err
	}
	if len(values) != 1 {
		return 0, fmt.Errorf("%w: path=%s section=[%s] values=%d want=1", ErrPVFMalformed, EventPVFPath, name, len(values))
	}
	return values[0], nil
}

func joustIntegerSection(document *dnfpvf.Document, name string) ([]int64, error) {
	if document == nil {
		return nil, fmt.Errorf("%w: nil document", ErrPVFMalformed)
	}
	count := 0
	for _, section := range document.Sections {
		if strings.EqualFold(strings.TrimSpace(section.Name), name) {
			count++
		}
	}
	if count != 1 {
		return nil, fmt.Errorf("%w: path=%s section=[%s] count=%d want=1", ErrPVFMalformed, EventPVFPath, name, count)
	}
	tokens, ok := document.Section(name)
	if !ok {
		return nil, fmt.Errorf("%w: path=%s section=[%s] unreadable", ErrPVFMalformed, EventPVFPath, name)
	}
	values := make([]int64, 0, len(tokens))
	for _, token := range tokens {
		if token.Kind != dnfpvf.TokenInt {
			return nil, fmt.Errorf("%w: path=%s section=[%s] token=%q is not an integer", ErrPVFMalformed, EventPVFPath, name, token.Raw)
		}
		values = append(values, token.Int)
	}
	return values, nil
}

type joustRiderBuilder struct {
	rider         Rider
	indexSet      bool
	attackTypeSet bool
	nameSet       bool
	activeField   string
}

func joustRiders(document *dnfpvf.Document) ([]Rider, error) {
	if document == nil {
		return nil, fmt.Errorf("%w: nil document", ErrPVFMalformed)
	}
	riders := make([]Rider, 0, 12)
	seen := make(map[byte]struct{}, 12)
	var current *joustRiderBuilder
	for _, token := range document.Tokens {
		if token.Kind == dnfpvf.TokenSection {
			section := strings.ToLower(strings.TrimSpace(token.Value))
			switch section {
			case "knight":
				if current != nil {
					return nil, fmt.Errorf("%w: nested [knight] at line=%d", ErrPVFMalformed, token.Line)
				}
				current = &joustRiderBuilder{}
			case "/knight":
				if current == nil {
					return nil, fmt.Errorf("%w: unmatched [/knight] at line=%d", ErrPVFMalformed, token.Line)
				}
				if err := current.validate(); err != nil {
					return nil, err
				}
				if _, duplicate := seen[current.rider.ID]; duplicate {
					return nil, fmt.Errorf("%w: duplicate rider index=%d", ErrPVFMalformed, current.rider.ID)
				}
				seen[current.rider.ID] = struct{}{}
				riders = append(riders, cloneRider(current.rider))
				current = nil
			default:
				if current != nil {
					current.activeField = section
				}
			}
			continue
		}
		if current == nil {
			continue
		}
		if err := current.consume(token); err != nil {
			return nil, err
		}
	}
	if current != nil {
		return nil, fmt.Errorf("%w: unterminated [knight]", ErrPVFMalformed)
	}
	return riders, nil
}

func (b *joustRiderBuilder) consume(token dnfpvf.Token) error {
	if b == nil {
		return fmt.Errorf("%w: nil rider builder", ErrPVFMalformed)
	}
	switch b.activeField {
	case "index":
		value, err := joustByteToken(token, "index")
		if err != nil {
			return err
		}
		if b.indexSet {
			return fmt.Errorf("%w: duplicate rider index value", ErrPVFMalformed)
		}
		b.rider.ID = value
		b.indexSet = true
	case "attack type":
		value, err := joustByteToken(token, "attack type")
		if err != nil {
			return err
		}
		if b.attackTypeSet {
			return fmt.Errorf("%w: duplicate rider attack type", ErrPVFMalformed)
		}
		b.rider.AttackType = value
		b.attackTypeSet = true
	case "knight name":
		if token.Kind != dnfpvf.TokenString || strings.TrimSpace(token.Value) == "" || b.nameSet {
			return fmt.Errorf("%w: invalid rider name token=%q", ErrPVFMalformed, token.Raw)
		}
		b.rider.Name = token.Value
		b.nameSet = true
	case "win":
		value, err := joustUint16Token(token, "win")
		if err != nil {
			return err
		}
		b.rider.Win = append(b.rider.Win, value)
	case "loss":
		value, err := joustUint16Token(token, "loss")
		if err != nil {
			return err
		}
		b.rider.Loss = append(b.rider.Loss, value)
	}
	return nil
}

func (b *joustRiderBuilder) validate() error {
	if b == nil || !b.indexSet || !b.attackTypeSet || !b.nameSet || len(b.rider.Win) != 28 || len(b.rider.Loss) != 28 {
		return fmt.Errorf(
			"%w: rider index_set=%t attack_set=%t name_set=%t win=%d loss=%d",
			ErrPVFMalformed,
			b != nil && b.indexSet,
			b != nil && b.attackTypeSet,
			b != nil && b.nameSet,
			func() int {
				if b == nil {
					return 0
				}
				return len(b.rider.Win)
			}(),
			func() int {
				if b == nil {
					return 0
				}
				return len(b.rider.Loss)
			}(),
		)
	}
	if _, ok := attackTypeTableOffset(b.rider.AttackType); !ok {
		return fmt.Errorf("%w: rider=%d unsupported attack type=%d", ErrPVFMalformed, b.rider.ID, b.rider.AttackType)
	}
	if err := validateBattleTimelineProfiles(b.rider.ID, "win", b.rider.Win); err != nil {
		return err
	}
	if err := validateBattleTimelineProfiles(b.rider.ID, "loss", b.rider.Loss); err != nil {
		return err
	}
	return nil
}

func validateBattleTimelineProfiles(rider byte, field string, values []uint16) error {
	for group := 0; group < 4; group++ {
		previous := uint16(0)
		for position := 0; position < 7; position++ {
			value := values[group*7+position]
			if value < previous || value > 100 {
				return fmt.Errorf("%w: rider=%d %s group=%d position=%d timeline=%d previous=%d", ErrPVFMalformed, rider, field, group, position, value, previous)
			}
			previous = value
		}
		if previous == 0 {
			return fmt.Errorf("%w: rider=%d %s group=%d terminal timeline is zero", ErrPVFMalformed, rider, field, group)
		}
	}
	return nil
}

func attackTypeTableOffset(attackType byte) (int, bool) {
	switch attackType {
	case 0:
		return 0, true
	case 1:
		return 7, true
	case 27:
		return 14, true
	case 28:
		return 21, true
	default:
		return 0, false
	}
}

func (c *Catalog) rider(id byte) (Rider, bool) {
	if c == nil {
		return Rider{}, false
	}
	for _, rider := range c.riders {
		if rider.ID == id {
			return cloneRider(rider), true
		}
	}
	return Rider{}, false
}

func joustByteToken(token dnfpvf.Token, field string) (byte, error) {
	if token.Kind != dnfpvf.TokenInt || token.Int < 0 || token.Int > 255 {
		return 0, fmt.Errorf("%w: rider %s token=%q", ErrPVFMalformed, field, token.Raw)
	}
	return byte(token.Int), nil
}

func joustUint16Token(token dnfpvf.Token, field string) (uint16, error) {
	if token.Kind != dnfpvf.TokenInt || token.Int < 0 || token.Int > 65535 {
		return 0, fmt.Errorf("%w: rider %s token=%q", ErrPVFMalformed, field, token.Raw)
	}
	return uint16(token.Int), nil
}

func cloneRiders(source []Rider) []Rider {
	out := make([]Rider, len(source))
	for index := range source {
		out[index] = cloneRider(source[index])
	}
	return out
}

func cloneRider(source Rider) Rider {
	source.Win = append([]uint16(nil), source.Win...)
	source.Loss = append([]uint16(nil), source.Loss...)
	return source
}
