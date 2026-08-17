package redeem

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	platformdb "longheng.io/server/internal/platform/db"
)

type StateStore interface {
	Load(context.Context) (State, bool, error)
	Save(context.Context, State) error
	Close() error
}

type State struct {
	Codes  []Code         `json:"codes,omitempty"`
	Claims []ClaimResult  `json:"claims,omitempty"`
	Uses   map[string]int `json:"uses,omitempty"`
}

type PersistentStore struct {
	mu sync.Mutex
	// saveMu 串行化完整快照保存，避免慢后端把旧快照写回覆盖新状态。
	saveMu  sync.Mutex
	memory  *MemoryStore
	store   StateStore
	version uint64
}

func NewPersistentStore(ctx context.Context, store StateStore) (*PersistentStore, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if store == nil {
		return nil, ErrStoreRequired
	}
	memory := NewMemoryStore()
	if state, ok, err := store.Load(ctx); err != nil {
		return nil, err
	} else if ok {
		memory.importState(state)
	}
	return &PersistentStore{memory: memory, store: store}, nil
}

func (s *PersistentStore) PutCode(ctx context.Context, code Code) (Code, error) {
	if err := ctxErr(ctx); err != nil {
		return Code{}, err
	}
	if err := s.requireWritable(); err != nil {
		return Code{}, err
	}
	s.saveMu.Lock()
	s.mu.Lock()
	before := s.memory.exportState()
	result, err := s.memory.PutCode(ctx, code)
	if err != nil {
		s.mu.Unlock()
		s.saveMu.Unlock()
		return result, err
	}
	after := s.memory.exportState()
	s.version++
	version := s.version
	s.mu.Unlock()
	return result, s.persistAfterUnlock(ctx, before, after, version)
}

func (s *PersistentStore) Claim(ctx context.Context, request ClaimRequest, now time.Time) (ClaimResult, error) {
	if err := ctxErr(ctx); err != nil {
		return ClaimResult{}, err
	}
	if err := s.requireWritable(); err != nil {
		return ClaimResult{}, err
	}
	s.saveMu.Lock()
	s.mu.Lock()
	before := s.memory.exportState()
	result, err := s.memory.Claim(ctx, request, now)
	if err != nil || result.Duplicate {
		s.mu.Unlock()
		s.saveMu.Unlock()
		return result, err
	}
	after := s.memory.exportState()
	s.version++
	version := s.version
	s.mu.Unlock()
	return result, s.persistAfterUnlock(ctx, before, after, version)
}

func (s *PersistentStore) RollbackClaim(ctx context.Context, result ClaimResult) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil || s.memory == nil || s.store == nil {
		return ErrStoreRequired
	}
	s.saveMu.Lock()
	s.mu.Lock()
	before := s.memory.exportState()
	if err := s.memory.RollbackClaim(ctx, result); err != nil {
		s.mu.Unlock()
		s.saveMu.Unlock()
		return err
	}
	after := s.memory.exportState()
	s.version++
	version := s.version
	s.mu.Unlock()
	return s.persistAfterUnlock(ctx, before, after, version)
}

func (s *PersistentStore) Snapshot(ctx context.Context) (Snapshot, error) {
	if s == nil || s.memory == nil {
		return Snapshot{}, ErrStoreRequired
	}
	return s.memory.Snapshot(ctx)
}

func (s *PersistentStore) Close() error {
	if s == nil || s.store == nil {
		return nil
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	return s.store.Close()
}

func (s *PersistentStore) requireWritable() error {
	if s == nil || s.memory == nil || s.store == nil {
		return ErrStoreRequired
	}
	return nil
}

func (s *PersistentStore) persistAfterUnlock(ctx context.Context, before, after State, version uint64) error {
	if s == nil || s.memory == nil || s.store == nil {
		return ErrStoreRequired
	}
	defer s.saveMu.Unlock()
	// 礼包码扣次属于关键权益，仍需同步 durable；慢 I/O 不能占住内存状态锁。
	if saveErr := s.store.Save(ctx, after); saveErr != nil {
		s.mu.Lock()
		// 只回滚发起本次保存的版本，避免失败的旧保存覆盖后续成功领取或补码。
		if s.version == version {
			s.memory.importState(before)
			s.version++
		}
		s.mu.Unlock()
		return saveErr
	}
	return nil
}

type JSONFileStateStore struct {
	Path string
}

func NewJSONFileStateStore(path string) *JSONFileStateStore {
	return &JSONFileStateStore{Path: path}
}

func (s *JSONFileStateStore) Load(ctx context.Context) (State, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return State{}, false, err
	}
	if s == nil || s.Path == "" {
		return State{}, false, ErrStoreRequired
	}
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, false, nil
	}
	if err != nil {
		return State{}, false, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, false, err
	}
	return state, true, nil
}

func (s *JSONFileStateStore) Save(ctx context.Context, state State) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil || s.Path == "" {
		return ErrStoreRequired
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return platformdb.WriteFileAtomically(ctx, s.Path, data, 0o600)
}

func (s *JSONFileStateStore) Close() error {
	return nil
}

func (s *MemoryStore) exportState() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	codes := make([]Code, 0, len(s.codes))
	for _, code := range s.codes {
		codes = append(codes, cloneCode(code))
	}
	sort.Slice(codes, func(i, j int) bool { return codes[i].Code < codes[j].Code })
	claims := make([]ClaimResult, 0, len(s.claimsByKey))
	for _, claim := range s.claimsByKey {
		claim = cloneClaimResult(claim)
		claim.Duplicate = false
		claims = append(claims, claim)
	}
	sort.Slice(claims, func(i, j int) bool {
		if claims[i].Code != claims[j].Code {
			return claims[i].Code < claims[j].Code
		}
		if claims[i].AccountID != claims[j].AccountID {
			return claims[i].AccountID < claims[j].AccountID
		}
		return claims[i].IdempotencyKey < claims[j].IdempotencyKey
	})
	return State{Codes: codes, Claims: claims, Uses: cloneIntMap(s.uses)}
}

func (s *MemoryStore) importState(state State) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.codes = make(map[string]Code, len(state.Codes))
	s.claimsByKey = make(map[string]ClaimResult, len(state.Claims))
	s.claimsByAccount = make(map[string]int, len(state.Claims))
	s.uses = cloneIntMap(state.Uses)
	if s.uses == nil {
		s.uses = make(map[string]int)
	}
	replayedUses := make(map[string]int)
	for _, code := range state.Codes {
		code = normalizeCode(code)
		if code.Code != "" {
			s.codes[code.Code] = cloneCode(code)
		}
	}
	for _, claim := range state.Claims {
		claim = cloneClaimResult(claim)
		claim.Code = strings.TrimSpace(claim.Code)
		claim.AccountID = strings.TrimSpace(claim.AccountID)
		claim.IdempotencyKey = strings.TrimSpace(claim.IdempotencyKey)
		if claim.Code == "" || claim.AccountID == "" || claim.IdempotencyKey == "" {
			continue
		}
		claim.Duplicate = false
		request := ClaimRequest{Code: claim.Code, AccountID: claim.AccountID, IdempotencyKey: claim.IdempotencyKey}
		s.claimsByKey[claimLookupKey(request)] = claim
		s.claimsByAccount[claim.Code+"\x00"+claim.AccountID]++
		replayedUses[claim.Code]++
	}
	for code, uses := range replayedUses {
		if uses > s.uses[code] {
			s.uses[code] = uses
		}
	}
	for _, claim := range state.Claims {
		if claim.Uses > s.uses[strings.TrimSpace(claim.Code)] {
			s.uses[strings.TrimSpace(claim.Code)] = claim.Uses
		}
	}
}
