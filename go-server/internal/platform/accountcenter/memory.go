package accountcenter

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"sync"
	"time"
)

type TokenIssuer func(context.Context, Account, Shard, GateNode) (string, error)

type CenterOptions struct {
	Now         func() time.Time
	TokenIssuer TokenIssuer
	Store       Store
}

type Center struct {
	mu         sync.Mutex
	saveMu     sync.Mutex
	now        func() time.Time
	issuer     TokenIssuer
	store      Store
	version    uint64
	nextNum    int64
	accounts   map[string]Account
	identity   map[string]string
	bans       map[string]BanRecord
	allows     map[string]AllowRecord
	banIndex   map[string]map[string]struct{}
	allowIndex map[string]map[string]struct{}
	shards     map[string]Shard
	gates      map[string]GateNode
}

func NewMemoryCenter(options CenterOptions) *Center {
	center, err := NewCenter(context.Background(), options)
	if err != nil {
		panic(err)
	}
	return center
}

func NewCenter(ctx context.Context, options CenterOptions) (*Center, error) {
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	issuer := options.TokenIssuer
	if issuer == nil {
		issuer = randomTokenIssuer
	}
	center := &Center{
		now:        now,
		issuer:     issuer,
		store:      options.Store,
		nextNum:    100000,
		accounts:   make(map[string]Account),
		identity:   make(map[string]string),
		bans:       make(map[string]BanRecord),
		allows:     make(map[string]AllowRecord),
		banIndex:   make(map[string]map[string]struct{}),
		allowIndex: make(map[string]map[string]struct{}),
		shards:     make(map[string]Shard),
		gates:      make(map[string]GateNode),
	}
	if options.Store != nil {
		exported, ok, err := options.Store.Load(ctx)
		if err != nil {
			return nil, fmt.Errorf("%w: load account center state: %w", ErrStoreUnavailable, err)
		}
		if ok {
			center.importLocked(exported)
		}
	}
	return center, nil
}

func (c *Center) Login(ctx context.Context, request LoginRequest) (LoginResult, error) {
	if err := ctxErr(ctx); err != nil {
		return LoginResult{}, err
	}
	identity, err := normalizeIdentity(request.Identity)
	if err != nil {
		return LoginResult{}, err
	}
	c.saveMu.Lock()
	c.mu.Lock()
	account, created, err := c.findOrCreateAcct(identity)
	if err != nil {
		c.mu.Unlock()
		c.saveMu.Unlock()
		return LoginResult{}, err
	}
	shard, err := c.selectShardLocked(request.PreferredShardID)
	if err != nil {
		if created {
			c.rollbackCreated(account, identity)
		}
		c.mu.Unlock()
		c.saveMu.Unlock()
		return LoginResult{}, err
	}
	if err := c.checkAccessLocked(account.ID, shard.ID, request.RequireAllowList); err != nil {
		if created {
			c.rollbackCreated(account, identity)
		}
		c.mu.Unlock()
		c.saveMu.Unlock()
		return LoginResult{}, err
	}
	gate, err := c.selectGateLocked(account.ID, shard)
	if err != nil {
		if created {
			c.rollbackCreated(account, identity)
		}
		c.mu.Unlock()
		c.saveMu.Unlock()
		return LoginResult{}, err
	}
	if created {
		exported, version := c.exportForSaveLocked()
		c.mu.Unlock()
		if err := c.persistAfterUnlock(ctx, exported, version, func() {
			c.rollbackCreated(account, identity)
		}); err != nil {
			return LoginResult{}, err
		}
	} else {
		c.mu.Unlock()
		c.saveMu.Unlock()
	}
	token, err := c.issuer(ctx, account, shard, gate)
	if err != nil {
		return LoginResult{}, fmt.Errorf("%w: %w", ErrLoginTokenUnavailable, err)
	}
	return LoginResult{
		Account: account,
		Shard:   shard,
		Gate: GateSelection{
			Address:    gate.Address,
			LoginToken: token,
			NodeID:     gate.NodeID,
		},
		Created: created,
	}, nil
}

func (c *Center) Bind(ctx context.Context, request BindRequest) (Account, error) {
	if err := ctxErr(ctx); err != nil {
		return Account{}, err
	}
	identity, err := normalizeIdentity(request.Identity)
	if err != nil {
		return Account{}, err
	}
	accountID := strings.TrimSpace(request.AccountID)
	if accountID == "" {
		return Account{}, ErrAccountNotFound
	}
	id := identityID(identity)
	c.saveMu.Lock()
	c.mu.Lock()
	account, ok := c.accounts[accountID]
	if !ok {
		c.mu.Unlock()
		c.saveMu.Unlock()
		return Account{}, ErrAccountNotFound
	}
	if bound, ok := c.identity[id]; ok && bound != accountID {
		c.mu.Unlock()
		c.saveMu.Unlock()
		return Account{}, ErrIdentityConflict
	}
	if _, ok := c.identity[id]; !ok {
		previous := cloneAccount(account)
		account.Bindings = append(account.Bindings, identity)
		account.UpdatedAt = c.now().UTC()
		c.accounts[accountID] = cloneAccount(account)
		c.identity[id] = accountID
		exported, version := c.exportForSaveLocked()
		c.mu.Unlock()
		if err := c.persistAfterUnlock(ctx, exported, version, func() {
			c.accounts[accountID] = previous
			delete(c.identity, id)
		}); err != nil {
			return Account{}, err
		}
		return cloneAccount(account), nil
	}
	out := cloneAccount(c.accounts[accountID])
	c.mu.Unlock()
	c.saveMu.Unlock()
	return out, nil
}

func (c *Center) SetBan(ctx context.Context, record BanRecord) error {
	subject := strings.TrimSpace(record.Subject)
	if subject == "" {
		return ErrAccountNotFound
	}
	record.Subject = subject
	record.Scope = normalizeScope(record.Scope)
	if record.CreatedAt.IsZero() {
		record.CreatedAt = c.now().UTC()
	}
	c.saveMu.Lock()
	c.mu.Lock()
	key := accessKey(record.Subject, record.Scope)
	previous, hadPrevious := c.bans[key]
	if hadPrevious {
		c.unindexAccessLocked(c.banIndex, key, previous.Subject)
	}
	c.bans[accessKey(record.Subject, record.Scope)] = cloneBanRecord(record)
	c.indexAccessLocked(c.banIndex, key, record.Subject)
	exported, version := c.exportForSaveLocked()
	c.mu.Unlock()
	return c.persistAfterUnlock(ctx, exported, version, func() {
		c.unindexAccessLocked(c.banIndex, key, record.Subject)
		if hadPrevious {
			c.bans[key] = previous
			c.indexAccessLocked(c.banIndex, key, previous.Subject)
		} else {
			delete(c.bans, key)
		}
	})
}

func (c *Center) SetAllow(ctx context.Context, record AllowRecord) error {
	subject := strings.TrimSpace(record.Subject)
	if subject == "" {
		return ErrAccountNotFound
	}
	record.Subject = subject
	record.Scope = normalizeScope(record.Scope)
	if record.CreatedAt.IsZero() {
		record.CreatedAt = c.now().UTC()
	}
	c.saveMu.Lock()
	c.mu.Lock()
	key := accessKey(record.Subject, record.Scope)
	previous, hadPrevious := c.allows[key]
	if hadPrevious {
		c.unindexAccessLocked(c.allowIndex, key, previous.Subject)
	}
	c.allows[key] = cloneAllowRecord(record)
	c.indexAccessLocked(c.allowIndex, key, record.Subject)
	exported, version := c.exportForSaveLocked()
	c.mu.Unlock()
	return c.persistAfterUnlock(ctx, exported, version, func() {
		c.unindexAccessLocked(c.allowIndex, key, record.Subject)
		if hadPrevious {
			c.allows[key] = previous
			c.indexAccessLocked(c.allowIndex, key, previous.Subject)
		} else {
			delete(c.allows, key)
		}
	})
}

func (c *Center) SetShards(ctx context.Context, shards []Shard) error {
	c.saveMu.Lock()
	c.mu.Lock()
	previous := c.shards
	c.shards = make(map[string]Shard, len(shards))
	for _, shard := range shards {
		shard.ID = strings.TrimSpace(shard.ID)
		if shard.ID == "" {
			continue
		}
		shard.State = strings.TrimSpace(shard.State)
		if shard.State == "" {
			shard.State = ShardOpen
		}
		if shard.Weight <= 0 {
			shard.Weight = 1
		}
		c.shards[shard.ID] = shard
	}
	exported, version := c.exportForSaveLocked()
	c.mu.Unlock()
	return c.persistAfterUnlock(ctx, exported, version, func() {
		c.shards = previous
	})
}

func (c *Center) SetGates(ctx context.Context, gates []GateNode) error {
	c.saveMu.Lock()
	c.mu.Lock()
	previous := c.gates
	c.gates = make(map[string]GateNode, len(gates))
	for _, gate := range gates {
		gate.NodeID = strings.TrimSpace(gate.NodeID)
		gate.Address = strings.TrimSpace(gate.Address)
		if gate.NodeID == "" || gate.Address == "" {
			continue
		}
		if gate.Weight <= 0 {
			gate.Weight = 1
		}
		c.gates[gate.NodeID] = gate
	}
	exported, version := c.exportForSaveLocked()
	c.mu.Unlock()
	return c.persistAfterUnlock(ctx, exported, version, func() {
		c.gates = previous
	})
}

func (c *Center) Snapshot() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return map[string]any{
		"accounts": len(c.accounts),
		"bindings": len(c.identity),
		"bans":     len(c.bans),
		"allows":   len(c.allows),
		"shards":   len(c.shards),
		"gates":    len(c.gates),
	}
}

func (c *Center) GetAccount(ctx context.Context, accountID string) (Account, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return Account{}, false, err
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return Account{}, false, ErrAccountNotFound
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	account, ok := c.accounts[accountID]
	if !ok {
		return Account{}, false, nil
	}
	return cloneAccount(account), true, nil
}

func (c *Center) FindAccountByIdentity(ctx context.Context, identity Identity) (Account, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return Account{}, false, err
	}
	normalized, err := normalizeIdentity(identity)
	if err != nil {
		return Account{}, false, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	accountID, ok := c.identity[identityID(normalized)]
	if !ok {
		return Account{}, false, nil
	}
	account, ok := c.accounts[accountID]
	if !ok {
		return Account{}, false, nil
	}
	return cloneAccount(account), true, nil
}

func (c *Center) AccountAccess(ctx context.Context, accountID string) (AccountAccess, error) {
	if err := ctxErr(ctx); err != nil {
		return AccountAccess{}, err
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return AccountAccess{}, ErrAccountNotFound
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	access := AccountAccess{}
	for key := range c.banIndex[accountID] {
		if record, ok := c.bans[key]; ok {
			access.Bans = append(access.Bans, cloneBanRecord(record))
		}
	}
	for key := range c.allowIndex[accountID] {
		if record, ok := c.allows[key]; ok {
			access.Allows = append(access.Allows, cloneAllowRecord(record))
		}
	}
	sort.Slice(access.Bans, func(i, j int) bool {
		return accessKey(access.Bans[i].Subject, access.Bans[i].Scope) < accessKey(access.Bans[j].Subject, access.Bans[j].Scope)
	})
	sort.Slice(access.Allows, func(i, j int) bool {
		return accessKey(access.Allows[i].Subject, access.Allows[i].Scope) < accessKey(access.Allows[j].Subject, access.Allows[j].Scope)
	})
	return access, nil
}

func (c *Center) Export() Export {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.exportLocked()
}

func (c *Center) exportLocked() Export {
	out := Export{
		Accounts: make([]Account, 0, len(c.accounts)),
		Bans:     make([]BanRecord, 0, len(c.bans)),
		Allows:   make([]AllowRecord, 0, len(c.allows)),
		Shards:   make([]Shard, 0, len(c.shards)),
		Gates:    make([]GateNode, 0, len(c.gates)),
	}
	for _, account := range c.accounts {
		out.Accounts = append(out.Accounts, cloneAccount(account))
	}
	for _, ban := range c.bans {
		out.Bans = append(out.Bans, cloneBanRecord(ban))
	}
	for _, allow := range c.allows {
		out.Allows = append(out.Allows, cloneAllowRecord(allow))
	}
	for _, shard := range c.shards {
		out.Shards = append(out.Shards, shard)
	}
	for _, gate := range c.gates {
		out.Gates = append(out.Gates, gate)
	}
	sort.Slice(out.Accounts, func(i, j int) bool { return out.Accounts[i].ID < out.Accounts[j].ID })
	sort.Slice(out.Bans, func(i, j int) bool {
		return accessKey(out.Bans[i].Subject, out.Bans[i].Scope) < accessKey(out.Bans[j].Subject, out.Bans[j].Scope)
	})
	sort.Slice(out.Allows, func(i, j int) bool {
		return accessKey(out.Allows[i].Subject, out.Allows[i].Scope) < accessKey(out.Allows[j].Subject, out.Allows[j].Scope)
	})
	sort.Slice(out.Shards, func(i, j int) bool { return out.Shards[i].ID < out.Shards[j].ID })
	sort.Slice(out.Gates, func(i, j int) bool { return out.Gates[i].NodeID < out.Gates[j].NodeID })
	return out
}

func (c *Center) importLocked(exported Export) {
	c.accounts = make(map[string]Account, len(exported.Accounts))
	c.identity = make(map[string]string)
	c.bans = make(map[string]BanRecord, len(exported.Bans))
	c.allows = make(map[string]AllowRecord, len(exported.Allows))
	c.banIndex = make(map[string]map[string]struct{})
	c.allowIndex = make(map[string]map[string]struct{})
	c.shards = make(map[string]Shard, len(exported.Shards))
	c.gates = make(map[string]GateNode, len(exported.Gates))
	c.nextNum = 100000
	for _, account := range exported.Accounts {
		account = cloneAccount(account)
		c.accounts[account.ID] = account
		if account.NumID > c.nextNum {
			c.nextNum = account.NumID
		}
		for _, identity := range account.Bindings {
			normalized, err := normalizeIdentity(identity)
			if err != nil {
				continue
			}
			c.identity[identityID(normalized)] = account.ID
		}
	}
	for _, ban := range exported.Bans {
		ban.Subject = strings.TrimSpace(ban.Subject)
		if ban.Subject == "" {
			continue
		}
		ban.Scope = normalizeScope(ban.Scope)
		key := accessKey(ban.Subject, ban.Scope)
		c.bans[key] = cloneBanRecord(ban)
		c.indexAccessLocked(c.banIndex, key, ban.Subject)
	}
	for _, allow := range exported.Allows {
		allow.Subject = strings.TrimSpace(allow.Subject)
		if allow.Subject == "" {
			continue
		}
		allow.Scope = normalizeScope(allow.Scope)
		key := accessKey(allow.Subject, allow.Scope)
		c.allows[key] = cloneAllowRecord(allow)
		c.indexAccessLocked(c.allowIndex, key, allow.Subject)
	}
	for _, shard := range exported.Shards {
		shard.ID = strings.TrimSpace(shard.ID)
		if shard.ID == "" {
			continue
		}
		if shard.State == "" {
			shard.State = ShardOpen
		}
		c.shards[shard.ID] = shard
	}
	for _, gate := range exported.Gates {
		gate.NodeID = strings.TrimSpace(gate.NodeID)
		if gate.NodeID == "" {
			continue
		}
		c.gates[gate.NodeID] = gate
	}
}

func (c *Center) exportForSaveLocked() (Export, uint64) {
	c.version++
	return c.exportLocked(), c.version
}

// saveMu 固定先于 mu 获取，避免落盘失败回滚覆盖后续已提交写入。
func (c *Center) persistAfterUnlock(ctx context.Context, exported Export, version uint64, rollback func()) error {
	defer c.saveMu.Unlock()
	if c.store == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := c.store.Save(ctx, exported); err != nil {
		c.mu.Lock()
		if c.version == version && rollback != nil {
			rollback()
			c.version++
		}
		c.mu.Unlock()
		return fmt.Errorf("%w: save account center state: %w", ErrStoreUnavailable, err)
	}
	return nil
}

func (c *Center) Close() error {
	if c == nil || c.store == nil {
		return nil
	}
	c.saveMu.Lock()
	defer c.saveMu.Unlock()
	if closer, ok := c.store.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

func (c *Center) findOrCreateAcct(identity Identity) (Account, bool, error) {
	id := identityID(identity)
	if accountID, ok := c.identity[id]; ok {
		return cloneAccount(c.accounts[accountID]), false, nil
	}
	now := c.now().UTC()
	c.nextNum++
	account := Account{
		ID:        fmt.Sprintf("acc-%d", c.nextNum),
		NumID:     c.nextNum,
		Bindings:  []Identity{identity},
		CreatedAt: now,
		UpdatedAt: now,
	}
	c.accounts[account.ID] = account
	c.identity[id] = account.ID
	return cloneAccount(account), true, nil
}

func (c *Center) rollbackCreated(account Account, identity Identity) {
	delete(c.accounts, account.ID)
	delete(c.identity, identityID(identity))
	if account.NumID == c.nextNum {
		c.nextNum--
	}
}

func (c *Center) indexAccessLocked(index map[string]map[string]struct{}, key, subject string) {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return
	}
	items := index[subject]
	if items == nil {
		items = make(map[string]struct{})
		index[subject] = items
	}
	items[key] = struct{}{}
}

func (c *Center) unindexAccessLocked(index map[string]map[string]struct{}, key, subject string) {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return
	}
	items := index[subject]
	if len(items) == 0 {
		return
	}
	delete(items, key)
	if len(items) == 0 {
		delete(index, subject)
	}
}

func (c *Center) selectShardLocked(preferred string) (Shard, error) {
	preferred = strings.TrimSpace(preferred)
	if preferred != "" {
		shard, ok := c.shards[preferred]
		if !ok {
			return Shard{}, ErrShardNotFound
		}
		if shard.State == ShardClosed {
			return Shard{}, ErrShardClosed
		}
		return shard, nil
	}
	shards := make([]Shard, 0, len(c.shards))
	for _, shard := range c.shards {
		if shard.State != ShardClosed {
			shards = append(shards, shard)
		}
	}
	if len(shards) == 0 {
		return Shard{}, ErrShardNotFound
	}
	sort.Slice(shards, func(i, j int) bool {
		if shards[i].Weight != shards[j].Weight {
			return shards[i].Weight > shards[j].Weight
		}
		return shards[i].ID < shards[j].ID
	})
	return shards[0], nil
}

func (c *Center) selectGateLocked(accountID string, shard Shard) (GateNode, error) {
	candidates := make([]GateNode, 0, len(c.gates))
	for _, gate := range c.gates {
		if gate.Draining || gate.Address == "" {
			continue
		}
		if shard.GateGroupID != "" && gate.GateGroupID != "" && shard.GateGroupID != gate.GateGroupID {
			continue
		}
		if len(gate.ShardIDs) > 0 && !containsString(gate.ShardIDs, shard.ID) {
			continue
		}
		candidates = append(candidates, gate)
	}
	if len(candidates) == 0 {
		return GateNode{}, ErrGateUnavailable
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].NodeID < candidates[j].NodeID })
	idx := int(hashString(accountID+"|"+shard.ID) % uint64(len(candidates))) //nolint:gosec // G115：取模结果严格小于 candidates 长度，可安全作为 slice 下标。
	return candidates[idx], nil
}

func (c *Center) checkAccessLocked(accountID, shardID string, requireAllow bool) error {
	now := c.now().UTC()
	for _, scope := range []string{"global", "shard:" + shardID} {
		if ban, ok := c.bans[accessKey(accountID, scope)]; ok {
			if ban.Until.IsZero() || ban.Until.After(now) {
				return ErrAccountBanned
			}
		}
	}
	if !requireAllow {
		return nil
	}
	if _, ok := c.allows[accessKey(accountID, "global")]; ok {
		return nil
	}
	if _, ok := c.allows[accessKey(accountID, "shard:"+shardID)]; ok {
		return nil
	}
	return ErrAllowListRequired
}

func normalizeIdentity(identity Identity) (Identity, error) {
	identity.Kind = strings.ToLower(strings.TrimSpace(identity.Kind))
	identity.Issuer = strings.TrimSpace(identity.Issuer)
	identity.Subject = strings.TrimSpace(identity.Subject)
	identity.Email = strings.ToLower(strings.TrimSpace(identity.Email))
	identity.Channel = strings.ToLower(strings.TrimSpace(identity.Channel))
	identity.DeviceID = strings.TrimSpace(identity.DeviceID)
	switch identity.Kind {
	case IdentityDevice:
		if identity.Subject == "" {
			identity.Subject = identity.DeviceID
		}
	case IdentityEmail:
		if identity.Subject == "" {
			identity.Subject = identity.Email
		}
	case IdentityOIDC:
		if identity.Issuer == "" || identity.Subject == "" {
			return Identity{}, ErrInvalidIdentity
		}
	case IdentityChannel:
		if identity.Issuer == "" {
			identity.Issuer = identity.Channel
		}
	}
	if identity.Kind == "" || identity.Subject == "" {
		return Identity{}, ErrInvalidIdentity
	}
	return identity, nil
}

func identityID(identity Identity) string {
	identity, _ = normalizeIdentity(identity)
	return "v1:" + encodeIdentityPart(identity.Kind) + ":" + encodeIdentityPart(identity.Issuer) + ":" + encodeIdentityPart(identity.Subject)
}

func encodeIdentityPart(value string) string {
	if value == "" {
		return "_"
	}
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func normalizeScope(scope string) string {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return "global"
	}
	return scope
}

func accessKey(subject, scope string) string {
	return "v1:" + encodeIdentityPart(strings.TrimSpace(subject)) + ":" + encodeIdentityPart(normalizeScope(scope))
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func hashString(value string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(value))
	return h.Sum64()
}

func randomTokenIssuer(_ context.Context, account Account, shard Shard, _ GateNode) (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return "lh-login-" + account.ID + "-" + shard.ID + "-" + hex.EncodeToString(buf[:]), nil
}

func cloneAccount(account Account) Account {
	bindings := make([]Identity, 0, len(account.Bindings))
	for _, identity := range account.Bindings {
		bindings = append(bindings, cloneIdentity(identity))
	}
	account.Bindings = bindings
	account.Metadata = cloneStringMap(account.Metadata)
	return account
}

func cloneIdentity(identity Identity) Identity {
	identity.Metadata = cloneStringMap(identity.Metadata)
	return identity
}

func cloneBanRecord(record BanRecord) BanRecord {
	record.Metadata = cloneStringMap(record.Metadata)
	return record
}

func cloneAllowRecord(record AllowRecord) AllowRecord {
	record.Metadata = cloneStringMap(record.Metadata)
	return record
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
