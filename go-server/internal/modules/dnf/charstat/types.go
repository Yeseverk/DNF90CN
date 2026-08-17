package charstat

import "errors"

const DefaultList = "character/character.lst"

var (
	ErrSourceRequired = errors.New("dnf character stat source is required")
	ErrListEmpty      = errors.New("dnf character stat list is empty")
	ErrJobMissing     = errors.New("dnf character stat job is missing")
	ErrGrowthMissing  = errors.New("dnf character stat growth is missing")
)

// Source 表示已经加载到内存的 PVF 文本来源。
type Source interface {
	ReadText(relativePath string) (string, error)
}

type Options struct {
	ListPath string
}

// Vector 是客户端 USERINFO 属性块使用的 10 倍整数属性。
type Vector struct {
	HPMax             int64
	MPMax             int64
	Strength          int64
	Intelligence      int64
	Vitality          int64
	Spirit            int64
	PhysicalAttack    int64
	PhysicalDefense   int64
	MagicalAttack     int64
	MagicalDefense    int64
	IndependentAttack int64
	FireResistance    int64
	WaterResistance   int64
	DarkResistance    int64
	LightResistance   int64
	InventoryLimit    int64
	HPRegenSpeed      int64
	MPRegenSpeed      int64
	MoveSpeed         int64
	AttackSpeed       int64
	CastSpeed         int64
	HitRecovery       int64
	JumpPower         int64
	Weight            int64
}

func (v *Vector) add(other Vector) {
	v.HPMax += other.HPMax
	v.MPMax += other.MPMax
	v.Strength += other.Strength
	v.Intelligence += other.Intelligence
	v.Vitality += other.Vitality
	v.Spirit += other.Spirit
	v.PhysicalAttack += other.PhysicalAttack
	v.PhysicalDefense += other.PhysicalDefense
	v.MagicalAttack += other.MagicalAttack
	v.MagicalDefense += other.MagicalDefense
	v.IndependentAttack += other.IndependentAttack
	v.FireResistance += other.FireResistance
	v.WaterResistance += other.WaterResistance
	v.DarkResistance += other.DarkResistance
	v.LightResistance += other.LightResistance
	v.InventoryLimit += other.InventoryLimit
	v.HPRegenSpeed += other.HPRegenSpeed
	v.MPRegenSpeed += other.MPRegenSpeed
	v.MoveSpeed += other.MoveSpeed
	v.AttackSpeed += other.AttackSpeed
	v.CastSpeed += other.CastSpeed
	v.HitRecovery += other.HitRecovery
	v.JumpPower += other.JumpPower
	v.Weight += other.Weight
}

// Add merges another stat vector into v. Callers outside this package use it
// to aggregate PVF character growth with item-derived PVF stats.
func (v *Vector) Add(other Vector) {
	if v == nil {
		return
	}
	v.add(other)
}

type jobTables struct {
	path      string
	base      Vector
	growtype  [7]Vector
	hasGrow   [7]bool
	awakening [7][3]Vector
	hasAwake  [7][3]bool
}

// Table 是按 job 编号索引的角色成长表。
type Table struct {
	jobs map[byte]jobTables
}

type Snapshot struct {
	Jobs int `json:"jobs"`
}

func (t *Table) Snapshot() Snapshot {
	if t == nil {
		return Snapshot{}
	}
	return Snapshot{Jobs: len(t.jobs)}
}

// DecodeGrowType 拆出客户端角色记录里的 grow_type 低 4 位转职和高 4 位觉醒。
func DecodeGrowType(growType byte) (first int, second int) {
	return int(growType & 0x0f), int((growType >> 4) & 0x0f)
}
