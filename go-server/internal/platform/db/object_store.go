package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrObjectCodecReq 表示对象字段编解码器缺失或不合法。
var ErrObjectCodecReq = errors.New("object field codec is required")

// ErrHashObjectStoreReq 表示 hash 对象存储本身缺失。
var ErrHashObjectStoreReq = errors.New("hash object store is required")

// ErrObjectRecordFactoryReq 表示对象记录构造函数缺失。
var ErrObjectRecordFactoryReq = errors.New("hash object new record function is required")

// ObjectFieldCodec 描述对象字段和 hash 字段之间的编解码规则。
type ObjectFieldCodec[T any, F comparable] struct {
	Field   F
	Name    string
	Marshal func(T) ([]byte, error)
	Apply   func(*T, []byte) error
}

// HashObjectStoreOptions 配置 hash 后端对象存储。
type HashObjectStoreOptions[T any, F comparable] struct {
	Backend      HashBackend
	Fields       FieldRegistry[F]
	Codecs       []ObjectFieldCodec[T, F]
	Key          KeyFunc[T]
	Clone        CloneFunc[T]
	NewRecord    func(string) T
	NormalizeKey func(string) string
	TTL          time.Duration
}

// HashObjectStore 把对象字段拆分保存到 HashBackend。
type HashObjectStore[T any, F comparable] struct {
	backend      HashBackend
	fields       FieldRegistry[F]
	codecs       map[F]objectFieldCodec[T]
	keyFn        KeyFunc[T]
	cloneFn      CloneFunc[T]
	newRecord    func(string) T
	normalizeKey func(string) string
	ttl          time.Duration
}

type objectFieldCodec[T any] struct {
	name    string
	marshal func(T) ([]byte, error)
	apply   func(*T, []byte) error
}

// NewHashObjectStore 创建 hash 对象存储。
func NewHashObjectStore[T any, F comparable](options HashObjectStoreOptions[T, F]) (*HashObjectStore[T, F], error) {
	if options.Backend == nil {
		return nil, ErrHashBackendRequired
	}
	if options.Key == nil {
		return nil, fmt.Errorf("%w: key function is nil", ErrRecordKeyRequired)
	}
	if options.NewRecord == nil {
		return nil, ErrObjectRecordFactoryReq
	}
	if options.Clone == nil {
		options.Clone = IdentityClone[T]
	}
	fields := options.Fields
	if len(fields.All()) == 0 && len(options.Codecs) > 0 {
		order := make([]F, 0, len(options.Codecs))
		for _, codec := range options.Codecs {
			order = append(order, codec.Field)
		}
		fields = NewFieldRegistry(order)
	}
	codecs, err := normObjCodecs(fields, options.Codecs)
	if err != nil {
		return nil, err
	}
	return &HashObjectStore[T, F]{
		backend:      options.Backend,
		fields:       fields,
		codecs:       codecs,
		keyFn:        options.Key,
		cloneFn:      options.Clone,
		newRecord:    options.NewRecord,
		normalizeKey: options.NormalizeKey,
		ttl:          options.TTL,
	}, nil
}

// Load 从 hash 后端读取并组装对象。
func (s *HashObjectStore[T, F]) Load(ctx context.Context, key string) (T, bool, error) {
	var zero T
	if err := s.ensureLoadReady(); err != nil {
		return zero, false, err
	}
	ctx = contextOrBackground(ctx)
	key = normalizeLookupKey(key, s.normalizeKey)
	if key == "" {
		return zero, false, ErrRecordKeyRequired
	}
	raw, found, err := s.backend.LoadHash(ctx, key, s.fieldNames(s.fields.All()))
	if err != nil || !found {
		return zero, found, err
	}
	record, err := s.decodeRecord(key, raw)
	if err != nil {
		return zero, false, err
	}
	return s.cloneFn(record), true, nil
}

// LoadBatch 批量读取并组装对象。
func (s *HashObjectStore[T, F]) LoadBatch(ctx context.Context, keys []string) (map[string]T, error) {
	if err := s.ensureLoadReady(); err != nil {
		return nil, err
	}
	ctx = contextOrBackground(ctx)
	keys = normalizeBatchKeys(keys, s.normalizeKey)
	if len(keys) == 0 {
		return map[string]T{}, ctx.Err()
	}
	if batcher, ok := s.backend.(HashBatchLoaderBackend); ok {
		fieldNames := s.fieldNames(s.fields.All())
		requests := make([]HashLoadRequest, 0, len(keys))
		for _, key := range keys {
			requests = append(requests, HashLoadRequest{
				Key:    key,
				Fields: fieldNames,
			})
		}
		results, err := batcher.LoadHashBatch(ctx, requests)
		if err != nil {
			return nil, err
		}
		out := make(map[string]T, len(results))
		for _, key := range keys {
			result := results[key]
			if !result.Found {
				continue
			}
			record, err := s.decodeRecord(key, result.Fields)
			if err != nil {
				return nil, err
			}
			out[key] = s.cloneFn(record)
		}
		return out, nil
	}
	out := make(map[string]T, len(keys))
	for _, key := range keys {
		record, ok, err := s.Load(ctx, key)
		if err != nil {
			return nil, err
		}
		if ok {
			out[key] = record
		}
	}
	return out, nil
}

// Save 保存对象的全部字段。
func (s *HashObjectStore[T, F]) Save(ctx context.Context, record T) error {
	if err := s.ensureSaveReady(); err != nil {
		return err
	}
	fields := s.fields.All()
	if len(fields) == 0 {
		return ErrAllFieldsRequired
	}
	return s.SaveFields(ctx, record, fields...)
}

// SaveFields 保存对象的指定字段。
func (s *HashObjectStore[T, F]) SaveFields(ctx context.Context, record T, fields ...F) error {
	if err := s.ensureSaveReady(); err != nil {
		return err
	}
	ctx = contextOrBackground(ctx)
	fields = s.fields.Normalize(fields)
	if len(fields) == 0 {
		return nil
	}
	key, err := s.recordKey(record)
	if err != nil {
		return err
	}
	hashFields, err := s.encodeFields(record, fields)
	if err != nil {
		return err
	}
	return s.backend.SaveHash(ctx, key, hashFields, s.ttl)
}

// SaveFieldBatch 批量保存对象字段。
func (s *HashObjectStore[T, F]) SaveFieldBatch(ctx context.Context, saves []FieldSave[T, F]) error {
	if err := s.ensureSaveReady(); err != nil {
		return err
	}
	ctx = contextOrBackground(ctx)
	batches := make([]HashSaveBatch, 0, len(saves))
	for _, save := range normalizeFieldSaves(s.fields.Normalize, saves) {
		key, err := s.recordKey(save.Record)
		if err != nil {
			return err
		}
		fields, err := s.encodeFields(save.Record, save.Fields)
		if err != nil {
			return err
		}
		if len(fields) == 0 {
			continue
		}
		batches = append(batches, HashSaveBatch{Key: key, Fields: fields, TTL: s.ttl})
	}
	var err error
	batches, err = mergeHashSaveBatches(batches)
	if err != nil {
		return err
	}
	if len(batches) == 0 {
		return nil
	}
	if batcher, ok := s.backend.(HashBatchBackend); ok {
		return batcher.SaveHashBatch(ctx, batches)
	}
	for _, batch := range batches {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.backend.SaveHash(ctx, batch.Key, batch.Fields, batch.TTL); err != nil {
			return err
		}
	}
	return nil
}

// Check 检查 hash 后端可用性。
func (s *HashObjectStore[T, F]) Check(ctx context.Context) error {
	if err := s.ensureBaseReady(); err != nil {
		return err
	}
	return Check(ctx, s.backend)
}

// Fields 返回对象存储支持的字段列表。
func (s *HashObjectStore[T, F]) Fields() []F {
	if s == nil {
		return nil
	}
	return s.fields.All()
}

// FieldNames 返回字段对应的 hash 字段名。
func (s *HashObjectStore[T, F]) FieldNames(fields ...F) []string {
	if s == nil {
		return nil
	}
	if len(fields) == 0 {
		return s.fieldNames(s.fields.All())
	}
	return s.fieldNames(s.fields.Normalize(fields))
}

func (s *HashObjectStore[T, F]) recordKey(record T) (string, error) {
	key, err := RecordKey(s.keyFn, record)
	if err != nil {
		return "", err
	}
	key = normalizeLookupKey(key, s.normalizeKey)
	if key == "" {
		return "", ErrRecordKeyRequired
	}
	return key, nil
}

func (s *HashObjectStore[T, F]) ensureBaseReady() error {
	if s == nil {
		return ErrHashObjectStoreReq
	}
	if s.backend == nil {
		return ErrHashBackendRequired
	}
	return nil
}

func (s *HashObjectStore[T, F]) ensureLoadReady() error {
	if err := s.ensureBaseReady(); err != nil {
		return err
	}
	if s.newRecord == nil {
		return ErrObjectRecordFactoryReq
	}
	s.ensureClone()
	fields := s.fields.All()
	if len(fields) == 0 {
		return ErrAllFieldsRequired
	}
	return s.ensureCodecs(fields, false, true)
}

func (s *HashObjectStore[T, F]) ensureSaveReady() error {
	if err := s.ensureBaseReady(); err != nil {
		return err
	}
	if s.keyFn == nil {
		return fmt.Errorf("%w: key function is nil", ErrRecordKeyRequired)
	}
	s.ensureClone()
	fields := s.fields.All()
	if len(fields) == 0 {
		return ErrAllFieldsRequired
	}
	return s.ensureCodecs(fields, true, false)
}

func (s *HashObjectStore[T, F]) ensureClone() {
	if s.cloneFn == nil {
		s.cloneFn = IdentityClone[T]
	}
}

func (s *HashObjectStore[T, F]) ensureCodecs(fields []F, requireMarshal bool, requireApply bool) error {
	for _, field := range fields {
		codec, ok := s.codecs[field]
		if !ok || strings.TrimSpace(codec.name) == "" {
			return fmt.Errorf("%w: %v", ErrObjectCodecReq, field)
		}
		if requireMarshal && codec.marshal == nil {
			return fmt.Errorf("%w: %v", ErrObjectCodecReq, field)
		}
		if requireApply && codec.apply == nil {
			return fmt.Errorf("%w: %v", ErrObjectCodecReq, field)
		}
	}
	return nil
}

func (s *HashObjectStore[T, F]) encodeFields(record T, fields []F) (map[string][]byte, error) {
	out := make(map[string][]byte, len(fields))
	for _, field := range fields {
		codec, ok := s.codecs[field]
		if !ok {
			return nil, fmt.Errorf("%w: %v", ErrObjectCodecReq, field)
		}
		data, err := codec.marshal(s.cloneFn(record))
		if err != nil {
			return nil, fmt.Errorf("marshal object field %q: %w", codec.name, err)
		}
		if data == nil {
			return nil, fmt.Errorf("marshal object field %q returned nil", codec.name)
		}
		out[codec.name] = append([]byte(nil), data...)
	}
	return out, nil
}

func (s *HashObjectStore[T, F]) decodeRecord(key string, raw map[string][]byte) (T, error) {
	record := s.newRecord(key)
	for _, field := range s.fields.All() {
		codec := s.codecs[field]
		data, ok := raw[codec.name]
		if !ok {
			continue
		}
		if err := codec.apply(&record, append([]byte(nil), data...)); err != nil {
			return record, fmt.Errorf("apply object field %q: %w", codec.name, err)
		}
	}
	return record, nil
}

func (s *HashObjectStore[T, F]) fieldNames(fields []F) []string {
	if len(fields) == 0 {
		return nil
	}
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if codec, ok := s.codecs[field]; ok {
			out = append(out, codec.name)
		}
	}
	return out
}

func normObjCodecs[T any, F comparable](fields FieldRegistry[F], codecs []ObjectFieldCodec[T, F]) (map[F]objectFieldCodec[T], error) {
	if len(fields.All()) == 0 {
		return nil, ErrAllFieldsRequired
	}
	out := make(map[F]objectFieldCodec[T], len(codecs))
	names := make(map[string]F, len(codecs))
	seenFields := make(map[F]struct{}, len(codecs))
	for _, codec := range codecs {
		if !fields.IsKnown(codec.Field) {
			return nil, fmt.Errorf("%w: unknown field %v", ErrObjectCodecReq, codec.Field)
		}
		if _, exists := seenFields[codec.Field]; exists {
			return nil, fmt.Errorf("duplicate object field codec: %v", codec.Field)
		}
		seenFields[codec.Field] = struct{}{}
		name := strings.TrimSpace(codec.Name)
		if name == "" || codec.Marshal == nil || codec.Apply == nil {
			return nil, fmt.Errorf("%w: %v", ErrObjectCodecReq, codec.Field)
		}
		if existing, exists := names[name]; exists && existing != codec.Field {
			return nil, fmt.Errorf("duplicate object field name %q", name)
		}
		names[name] = codec.Field
		out[codec.Field] = objectFieldCodec[T]{
			name:    name,
			marshal: codec.Marshal,
			apply:   codec.Apply,
		}
	}
	for _, field := range fields.All() {
		if _, ok := out[field]; !ok {
			return nil, fmt.Errorf("%w: %v", ErrObjectCodecReq, field)
		}
	}
	return out, nil
}
