package memory

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"longheng.io/server/internal/modules/dnf/repository"
	"longheng.io/server/internal/modules/dnf/repository/mysql"
	"strings"
	"sync"
	"testing"

	platformdb "longheng.io/server/internal/platform/db"
)

func TestMemoryCharacterProgressionUnitOfWorkCommitsBothAggregates(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	seedCharacterProgression(t, ctx, repos)

	err := repos.CharacterProgression.WithinCharacterProgression(ctx, "77", func(characters repository.CharacterRepository, skills repository.SkillRepository) error {
		character, ok, err := characters.Load(ctx, "77")
		if err != nil || !ok {
			return errors.New("character missing")
		}
		skill, ok, err := skills.Load(ctx, "77")
		if err != nil || !ok {
			return errors.New("skill missing")
		}
		character.Level = 3
		character.Stats["exp"] = 270
		skill.Points = repository.SkillPointState{TotalSP: 110, RemainingSP: 85, TotalTP: 5, RemainingTP: 4, SyncedLevel: 3}
		if err := repository.SaveCharacterFields(ctx, characters, character, repository.CharacterFieldBase, repository.CharacterFieldStats); err != nil {
			return err
		}
		return repository.SaveSkillFields(ctx, skills, skill, repository.SkillFieldPoints)
	})
	if err != nil {
		t.Fatalf("WithinCharacterProgression() error = %v", err)
	}

	character, _, _ := repos.Character.Load(ctx, "77")
	skill, _, _ := repos.Skill.Load(ctx, "77")
	if character.Level != 3 || character.Stats["exp"] != 270 {
		t.Fatalf("character progression = level %d exp %d", character.Level, character.Stats["exp"])
	}
	if skill.Points != (repository.SkillPointState{TotalSP: 110, RemainingSP: 85, TotalTP: 5, RemainingTP: 4, SyncedLevel: 3}) {
		t.Fatalf("skill points = %+v", skill.Points)
	}
}

func TestMemoryCharacterProgressionUnitOfWorkRollsBackCallback(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	seedCharacterProgression(t, ctx, repos)
	wantErr := errors.New("reject progression")

	err := repos.CharacterProgression.WithinCharacterProgression(ctx, "77", func(characters repository.CharacterRepository, skills repository.SkillRepository) error {
		character, _, _ := characters.Load(ctx, "77")
		skill, _, _ := skills.Load(ctx, "77")
		character.Level = 3
		character.Stats["exp"] = 270
		skill.Points.TotalSP = 110
		if err := characters.Save(ctx, character); err != nil {
			return err
		}
		if err := skills.Save(ctx, skill); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("WithinCharacterProgression() error = %v, want %v", err, wantErr)
	}
	assertOriginalCharacterProgression(t, ctx, repos.Character, repos.Skill)
}

func TestMemoryCharacterProgressionUnitOfWorkRestoresCharacterWhenSkillCommitFails(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	seedCharacterProgression(t, ctx, repos)
	wantErr := errors.New("skill commit failed")
	failingSkills := &mutatingProgressionSkillStore{SkillRepository: repos.Skill, saveErr: wantErr}
	uow := &memoryCharacterProgressionUnitOfWork{character: repos.Character, skills: failingSkills}

	err := uow.WithinCharacterProgression(ctx, "77", func(characters repository.CharacterRepository, skills repository.SkillRepository) error {
		character, _, _ := characters.Load(ctx, "77")
		skill, _, _ := skills.Load(ctx, "77")
		character.Level = 3
		character.Stats["exp"] = 270
		skill.Points.TotalSP = 110
		if err := characters.Save(ctx, character); err != nil {
			return err
		}
		return skills.Save(ctx, skill)
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("WithinCharacterProgression() error = %v, want %v", err, wantErr)
	}
	assertOriginalCharacterProgression(t, ctx, repos.Character, repos.Skill)
}

func TestMemoryCharacterProgressionUnitOfWorkRestoresMutatingCharacterCommitFailure(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	seedCharacterProgression(t, ctx, repos)
	wantErr := errors.New("character commit failed")
	failingCharacters := &mutatingProgressionCharacterStore{CharacterRepository: repos.Character, saveErr: wantErr}
	uow := &memoryCharacterProgressionUnitOfWork{character: failingCharacters, skills: repos.Skill}

	err := uow.WithinCharacterProgression(ctx, "77", func(characters repository.CharacterRepository, skills repository.SkillRepository) error {
		character, _, _ := characters.Load(ctx, "77")
		skill, _, _ := skills.Load(ctx, "77")
		character.Level = 3
		character.Stats["exp"] = 270
		skill.Points.TotalSP = 110
		if err := characters.Save(ctx, character); err != nil {
			return err
		}
		return skills.Save(ctx, skill)
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("WithinCharacterProgression() error = %v, want %v", err, wantErr)
	}
	assertOriginalCharacterProgression(t, ctx, repos.Character, repos.Skill)
}

func TestMemoryCharacterProgressionUnitOfWorkRejectsCrossCharacterAccess(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	seedCharacterProgression(t, ctx, repos)

	err := repos.CharacterProgression.WithinCharacterProgression(ctx, "77", func(characters repository.CharacterRepository, _ repository.SkillRepository) error {
		_, _, err := characters.Load(ctx, "78")
		return err
	})
	if !errors.Is(err, platformdb.ErrRecordKeyRequired) {
		t.Fatalf("cross-character error = %v, want ErrRecordKeyRequired", err)
	}
	assertOriginalCharacterProgression(t, ctx, repos.Character, repos.Skill)
}

func TestMemoryCharacterProgressionUnitOfWorkSerializesBothAggregates(t *testing.T) {
	ctx := context.Background()
	repos := NewMemoryGroup()
	seedCharacterProgression(t, ctx, repos)

	var wg sync.WaitGroup
	errs := make(chan error, 30)
	for range 30 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- repos.CharacterProgression.WithinCharacterProgression(ctx, "77", func(characters repository.CharacterRepository, skills repository.SkillRepository) error {
				character, _, err := characters.Load(ctx, "77")
				if err != nil {
					return err
				}
				skill, _, err := skills.Load(ctx, "77")
				if err != nil {
					return err
				}
				character.Stats["exp"]++
				skill.Points.TotalSP++
				skill.Points.RemainingSP++
				if err := characters.Save(ctx, character); err != nil {
					return err
				}
				return skills.Save(ctx, skill)
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent progression error = %v", err)
		}
	}
	character, _, _ := repos.Character.Load(ctx, "77")
	skill, _, _ := repos.Skill.Load(ctx, "77")
	if character.Stats["exp"] != 270 || skill.Points.TotalSP != 130 || skill.Points.RemainingSP != 105 {
		t.Fatalf("serialized progression character=%+v points=%+v", character, skill.Points)
	}
}

func TestMySQLCharacterProgressionUnitOfWorkCommitsBothWrites(t *testing.T) {
	state := &progressionSQLState{}
	database := sql.OpenDB(progressionSQLConnector{state: state})
	t.Cleanup(func() { _ = database.Close() })
	repos, err := mysql.NewMySQLGroupFromDB(database, mysql.MySQLGroupOptions{
		DatabasePlan: repository.DatabasePlan{WriteDatabases: []string{"dnf_s1_w1"}},
	})
	if err != nil {
		t.Fatalf("NewMySQLGroupFromDB() error = %v", err)
	}

	err = repos.CharacterProgression.WithinCharacterProgression(context.Background(), "77", func(characters repository.CharacterRepository, skills repository.SkillRepository) error {
		character := repository.CharacterRecord{CharacterID: "77", AccountID: "dnf:1", Level: 2, Stats: map[string]int64{"exp": 120}}
		skill := repository.SkillRecord{CharacterID: "77", Points: repository.SkillPointState{TotalSP: 30, RemainingSP: 30, SyncedLevel: 2}}
		if err := repository.SaveCharacterFields(context.Background(), characters, character, repository.CharacterFieldBase, repository.CharacterFieldStats); err != nil {
			return err
		}
		return repository.SaveSkillFields(context.Background(), skills, skill, repository.SkillFieldPoints)
	})
	if err != nil {
		t.Fatalf("WithinCharacterProgression() error = %v", err)
	}

	begin, commit, rollback, queries := state.snapshot()
	if begin != 1 || commit != 1 || rollback != 0 {
		t.Fatalf("mysql transaction begin=%d commit=%d rollback=%d queries=%v", begin, commit, rollback, queries)
	}
	joined := strings.Join(queries, "\n")
	if !strings.Contains(joined, "`dnf_s1_w1`.`dnf_characters`") || !strings.Contains(joined, "`dnf_s1_w1`.`dnf_skills`") {
		t.Fatalf("mysql progression queries = %v", queries)
	}
}

func TestMySQLCharacterProgressionUnitOfWorkRollsBackAndScopesOwner(t *testing.T) {
	state := &progressionSQLState{}
	database := sql.OpenDB(progressionSQLConnector{state: state})
	t.Cleanup(func() { _ = database.Close() })
	repos, err := mysql.NewMySQLGroupFromDB(database, mysql.MySQLGroupOptions{
		DatabasePlan: repository.DatabasePlan{WriteDatabases: []string{"dnf_s1_w1"}},
	})
	if err != nil {
		t.Fatalf("NewMySQLGroupFromDB() error = %v", err)
	}

	err = repos.CharacterProgression.WithinCharacterProgression(context.Background(), "77", func(characters repository.CharacterRepository, _ repository.SkillRepository) error {
		return characters.Save(context.Background(), repository.CharacterRecord{CharacterID: "78"})
	})
	if !errors.Is(err, platformdb.ErrRecordKeyRequired) {
		t.Fatalf("cross-character error = %v, want ErrRecordKeyRequired", err)
	}
	begin, commit, rollback, queries := state.snapshot()
	if begin != 1 || commit != 0 || rollback != 1 || len(queries) != 0 {
		t.Fatalf("mysql rollback begin=%d commit=%d rollback=%d queries=%v", begin, commit, rollback, queries)
	}
}

func TestMySQLCharacterProgressionUnitOfWorkRollsBackBothExecutedWrites(t *testing.T) {
	state := &progressionSQLState{}
	database := sql.OpenDB(progressionSQLConnector{state: state})
	t.Cleanup(func() { _ = database.Close() })
	repos, err := mysql.NewMySQLGroupFromDB(database, mysql.MySQLGroupOptions{
		DatabasePlan: repository.DatabasePlan{WriteDatabases: []string{"dnf_s1_w1"}},
	})
	if err != nil {
		t.Fatalf("NewMySQLGroupFromDB() error = %v", err)
	}
	wantErr := errors.New("reject after both writes")

	err = repos.CharacterProgression.WithinCharacterProgression(context.Background(), "77", func(characters repository.CharacterRepository, skills repository.SkillRepository) error {
		character := repository.CharacterRecord{CharacterID: "77", AccountID: "dnf:1", Level: 2, Stats: map[string]int64{"exp": 120}}
		skill := repository.SkillRecord{CharacterID: "77", Points: repository.SkillPointState{TotalSP: 30, RemainingSP: 30, SyncedLevel: 2}}
		if err := repository.SaveCharacterFields(context.Background(), characters, character, repository.CharacterFieldBase, repository.CharacterFieldStats); err != nil {
			return err
		}
		if err := repository.SaveSkillFields(context.Background(), skills, skill, repository.SkillFieldPoints); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("WithinCharacterProgression() error = %v, want %v", err, wantErr)
	}
	begin, commit, rollback, queries := state.snapshot()
	if begin != 1 || commit != 0 || rollback != 1 {
		t.Fatalf("mysql rollback begin=%d commit=%d rollback=%d queries=%v", begin, commit, rollback, queries)
	}
}

func seedCharacterProgression(t *testing.T, ctx context.Context, repos repository.Group) {
	t.Helper()
	if repos.CharacterProgression == nil {
		t.Fatal("character progression unit of work is missing")
	}
	if err := repos.Character.Save(ctx, repository.CharacterRecord{
		CharacterID: "77",
		AccountID:   "dnf:1",
		Level:       2,
		Stats:       map[string]int64{"exp": 240},
	}); err != nil {
		t.Fatalf("seed character: %v", err)
	}
	if err := repos.Skill.Save(ctx, repository.SkillRecord{
		CharacterID: "77",
		Points:      repository.SkillPointState{TotalSP: 100, RemainingSP: 75, TotalTP: 3, RemainingTP: 2, SyncedLevel: 2},
	}); err != nil {
		t.Fatalf("seed skill: %v", err)
	}
}

func assertOriginalCharacterProgression(t *testing.T, ctx context.Context, characters repository.CharacterRepository, skills repository.SkillRepository) {
	t.Helper()
	character, _, _ := characters.Load(ctx, "77")
	skill, _, _ := skills.Load(ctx, "77")
	if character.Level != 2 || character.Stats["exp"] != 240 {
		t.Fatalf("character mutation escaped rollback: %+v", character)
	}
	if skill.Points != (repository.SkillPointState{TotalSP: 100, RemainingSP: 75, TotalTP: 3, RemainingTP: 2, SyncedLevel: 2}) {
		t.Fatalf("skill mutation escaped rollback: %+v", skill.Points)
	}
}

type mutatingProgressionSkillStore struct {
	repository.SkillRepository
	saveErr error
	failed  bool
}

type mutatingProgressionCharacterStore struct {
	repository.CharacterRepository
	saveErr error
	failed  bool
}

func (s *mutatingProgressionCharacterStore) Save(ctx context.Context, record repository.CharacterRecord) error {
	if !s.failed {
		s.failed = true
		if err := s.CharacterRepository.Save(ctx, record); err != nil {
			return err
		}
		return s.saveErr
	}
	return s.CharacterRepository.Save(ctx, record)
}

func (s *mutatingProgressionSkillStore) Save(ctx context.Context, record repository.SkillRecord) error {
	if !s.failed {
		s.failed = true
		if err := s.SkillRepository.Save(ctx, record); err != nil {
			return err
		}
		return s.saveErr
	}
	return s.SkillRepository.Save(ctx, record)
}

type progressionSQLState struct {
	mu       sync.Mutex
	begin    int
	commit   int
	rollback int
	queries  []string
}

func (s *progressionSQLState) snapshot() (int, int, int, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.begin, s.commit, s.rollback, append([]string(nil), s.queries...)
}

type progressionSQLConnector struct {
	state *progressionSQLState
}

func (c progressionSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return &progressionSQLConn{state: c.state}, nil
}

func (c progressionSQLConnector) Driver() driver.Driver {
	return progressionSQLDriver{state: c.state}
}

type progressionSQLDriver struct {
	state *progressionSQLState
}

func (d progressionSQLDriver) Open(string) (driver.Conn, error) {
	return &progressionSQLConn{state: d.state}, nil
}

type progressionSQLConn struct {
	state *progressionSQLState
}

func (c *progressionSQLConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare is not supported")
}

func (c *progressionSQLConn) Close() error { return nil }

func (c *progressionSQLConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *progressionSQLConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	c.state.mu.Lock()
	c.state.begin++
	c.state.mu.Unlock()
	return &progressionSQLTx{state: c.state}, nil
}

func (c *progressionSQLConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	c.state.mu.Lock()
	c.state.queries = append(c.state.queries, query)
	c.state.mu.Unlock()
	return driver.RowsAffected(1), nil
}

type progressionSQLTx struct {
	state *progressionSQLState
}

func (tx *progressionSQLTx) Commit() error {
	tx.state.mu.Lock()
	tx.state.commit++
	tx.state.mu.Unlock()
	return nil
}

func (tx *progressionSQLTx) Rollback() error {
	tx.state.mu.Lock()
	tx.state.rollback++
	tx.state.mu.Unlock()
	return nil
}
