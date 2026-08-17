package storageobject

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidKey      = errors.New("storage object key is invalid")
	ErrInvalidObject   = errors.New("storage object is invalid")
	ErrObjectNotFound  = errors.New("storage object not found")
	ErrVersionConflict = errors.New("storage object version conflict")
)

// Storage Object 只保存非权威轻量对象，权威玩家资产仍应走 Profile/MySQL。
const (
	PermissionNoAccess = 0
	PermissionOwner    = 1
	PermissionPublic   = 2
)

const (
	OwnerKindUser   = "user"
	OwnerKindSystem = "system"
)

type Key struct {
	Collection string `json:"collection"`
	Key        string `json:"key"`
	UserID     string `json:"user_id,omitempty"`
}

func (k Key) OwnerKind() string {
	if strings.TrimSpace(k.UserID) == "" {
		return OwnerKindSystem
	}
	return OwnerKindUser
}

type Object struct {
	Key             Key               `json:"key"`
	Value           json.RawMessage   `json:"value"`
	Index           map[string]string `json:"index,omitempty"`
	Version         string            `json:"version"`
	PermissionRead  int               `json:"permission_read,omitempty"`
	PermissionWrite int               `json:"permission_write,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

func (o Object) OwnerKind() string {
	return o.Key.OwnerKind()
}

type WriteRequest struct {
	Object         Object `json:"object"`
	IfMatchVersion string `json:"if_match_version,omitempty"`
	IfNoneMatch    bool   `json:"if_none_match,omitempty"`
}

type DeleteRequest struct {
	Key            Key    `json:"key"`
	IfMatchVersion string `json:"if_match_version,omitempty"`
}

type BatchReadRequest struct {
	Keys []Key `json:"keys"`
}

type BatchWriteRequest struct {
	Writes []WriteRequest `json:"writes"`
}

type BatchDeleteRequest struct {
	Deletes []DeleteRequest `json:"deletes"`
}

type BatchObjectResult struct {
	Object Object `json:"object,omitempty"`
	Found  bool   `json:"found,omitempty"`
	Error  string `json:"error,omitempty"`
}

type BatchDeleteResult struct {
	Key     Key    `json:"key"`
	Deleted bool   `json:"deleted,omitempty"`
	Error   string `json:"error,omitempty"`
}

type BatchReadResult struct {
	Results []BatchObjectResult `json:"results"`
}

type BatchWriteResult struct {
	Results []BatchObjectResult `json:"results"`
}

type BatchDeleteResults struct {
	Results []BatchDeleteResult `json:"results"`
}

type ListRequest struct {
	Collection string            `json:"collection"`
	UserID     string            `json:"user_id,omitempty"`
	Index      map[string]string `json:"index,omitempty"`
	Limit      int               `json:"limit,omitempty"`
	Cursor     string            `json:"cursor,omitempty"`
}

type ListResult struct {
	Objects    []Object `json:"objects"`
	NextCursor string   `json:"next_cursor,omitempty"`
}

type Store interface {
	Write(context.Context, WriteRequest) (Object, error)
	Read(context.Context, Key) (Object, bool, error)
	Delete(context.Context, Key, string) error
	List(context.Context, ListRequest) (ListResult, error)
}

type MemoryStore struct {
	mu      sync.Mutex
	now     func() time.Time
	objects map[string]Object
}

func NewMemoryStore(now func() time.Time) *MemoryStore {
	if now == nil {
		now = time.Now
	}
	return &MemoryStore{now: now, objects: make(map[string]Object)}
}

func (s *MemoryStore) Write(ctx context.Context, request WriteRequest) (Object, error) {
	if err := ctxErr(ctx); err != nil {
		return Object{}, err
	}
	if s == nil {
		return Object{}, ErrObjectNotFound
	}
	object, err := normalizeObject(request.Object, s.now().UTC())
	if err != nil {
		return Object{}, err
	}
	id := objectID(object.Key)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.objects == nil {
		s.objects = make(map[string]Object)
	}
	current, exists := s.objects[id]
	if request.IfNoneMatch && exists {
		return Object{}, ErrVersionConflict
	}
	if request.IfMatchVersion != "" {
		if !exists || current.Version != strings.TrimSpace(request.IfMatchVersion) {
			return Object{}, ErrVersionConflict
		}
	}
	if exists {
		object.CreatedAt = current.CreatedAt
	}
	object.Version = versionFor(object)
	s.objects[id] = cloneObject(object)
	return cloneObject(object), nil
}

func (s *MemoryStore) Read(ctx context.Context, key Key) (Object, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return Object{}, false, err
	}
	key, err := normalizeKey(key)
	if err != nil {
		return Object{}, false, err
	}
	if s == nil {
		return Object{}, false, nil
	}
	s.mu.Lock()
	object, ok := s.objects[objectID(key)]
	s.mu.Unlock()
	return cloneObject(object), ok, nil
}

func (s *MemoryStore) Delete(ctx context.Context, key Key, ifMatchVersion string) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	key, err := normalizeKey(key)
	if err != nil {
		return err
	}
	if s == nil {
		return ErrObjectNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := objectID(key)
	current, ok := s.objects[id]
	if !ok {
		return ErrObjectNotFound
	}
	if ifMatchVersion != "" && current.Version != strings.TrimSpace(ifMatchVersion) {
		return ErrVersionConflict
	}
	delete(s.objects, id)
	return nil
}

func (s *MemoryStore) List(ctx context.Context, request ListRequest) (ListResult, error) {
	if err := ctxErr(ctx); err != nil {
		return ListResult{}, err
	}
	collection := strings.TrimSpace(request.Collection)
	if collection == "" {
		return ListResult{}, ErrInvalidKey
	}
	userID := strings.TrimSpace(request.UserID)
	indexFilters := normalizeIndex(request.Index)
	limit := normalizeListLimit(request.Limit)
	cursor := strings.TrimSpace(request.Cursor)
	if s == nil {
		return ListResult{}, nil
	}
	s.mu.Lock()
	objects := make([]Object, 0, len(s.objects))
	for _, object := range s.objects {
		if object.Key.Collection != collection {
			continue
		}
		if userID != "" && object.Key.UserID != userID {
			continue
		}
		if !matchIndex(object, indexFilters) {
			continue
		}
		objects = append(objects, cloneObject(object))
	}
	s.mu.Unlock()
	sort.Slice(objects, func(i, j int) bool { return objectID(objects[i].Key) < objectID(objects[j].Key) })
	start := 0
	if cursor != "" {
		for idx, object := range objects {
			if objectID(object.Key) > cursor {
				start = idx
				break
			}
			start = idx + 1
		}
	}
	end := start + limit
	if end > len(objects) {
		end = len(objects)
	}
	result := ListResult{Objects: objects[start:end]}
	if end < len(objects) {
		result.NextCursor = objectID(objects[end-1].Key)
	}
	return result, nil
}

func normalizeObject(object Object, now time.Time) (Object, error) {
	key, err := normalizeKey(object.Key)
	if err != nil {
		return Object{}, err
	}
	if len(object.Value) == 0 || !json.Valid(object.Value) {
		return Object{}, ErrInvalidObject
	}
	object.Key = key
	object.Value = append(json.RawMessage(nil), object.Value...)
	object.Index = normalizeIndex(object.Index)
	object.PermissionRead = normalizePermission(object.PermissionRead)
	object.PermissionWrite = normalizePermission(object.PermissionWrite)
	if object.CreatedAt.IsZero() {
		object.CreatedAt = now
	} else {
		object.CreatedAt = object.CreatedAt.UTC()
	}
	object.UpdatedAt = now
	return object, nil
}

func normalizeListLimit(limit int) int {
	if limit <= 0 || limit > 100 {
		return 100
	}
	return limit
}

func normalizeKey(key Key) (Key, error) {
	key.Collection = strings.TrimSpace(key.Collection)
	key.Key = strings.TrimSpace(key.Key)
	key.UserID = strings.TrimSpace(key.UserID)
	if key.Collection == "" || key.Key == "" || !validKeyPart(key.Collection) || !validKeyPart(key.Key) || !validKeyPart(key.UserID) {
		return Key{}, ErrInvalidKey
	}
	return key, nil
}

func validKeyPart(value string) bool {
	return !strings.ContainsAny(value, "/\x00")
}

func normalizePermission(value int) int {
	switch value {
	case PermissionNoAccess, PermissionOwner, PermissionPublic:
		return value
	default:
		return PermissionOwner
	}
}

func normalizeIndex(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func matchIndex(object Object, filters map[string]string) bool {
	filters = normalizeIndex(filters)
	if len(filters) == 0 {
		return true
	}
	for key, value := range filters {
		if object.Index[key] != value {
			return false
		}
	}
	return true
}

func versionFor(object Object) string {
	indexJSON, _ := json.Marshal(normalizeIndex(object.Index))
	sum := sha256.Sum256([]byte(strings.Join([]string{
		objectID(object.Key),
		string(object.Value),
		string(indexJSON),
		object.UpdatedAt.Format(time.RFC3339Nano),
	}, "\x1f")))
	return hex.EncodeToString(sum[:12])
}

func objectID(key Key) string {
	key, _ = normalizeKey(key)
	return key.Collection + "/" + key.UserID + "/" + key.Key
}

func cloneObject(object Object) Object {
	object.Value = append(json.RawMessage(nil), object.Value...)
	object.Index = normalizeIndex(object.Index)
	return object
}

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
