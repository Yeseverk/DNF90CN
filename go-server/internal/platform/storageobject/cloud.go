package storageobject

import (
	"context"
	"encoding/json"
	"strconv"
	"time"
)

const archiveRecordMetaKey = "longheng-archive-record"

type CloudObjectClient interface {
	PutObject(context.Context, string, []byte, map[string]string) error
	GetObject(context.Context, string) ([]byte, map[string]string, bool, error)
	DeleteObject(context.Context, string) error
}

type CloudArchiveStore struct {
	client CloudObjectClient
	now    func() time.Time
}

func NewCloudArchiveStore(client CloudObjectClient, now func() time.Time) (*CloudArchiveStore, error) {
	if client == nil {
		return nil, ErrInvalidArchiveKey
	}
	if now == nil {
		now = time.Now
	}
	return &CloudArchiveStore{client: client, now: now}, nil
}

func (s *CloudArchiveStore) Put(ctx context.Context, key string, data []byte, meta map[string]string) (ArchiveRecord, error) {
	if err := ctxErr(ctx); err != nil {
		return ArchiveRecord{}, err
	}
	if s == nil || s.client == nil {
		return ArchiveRecord{}, ErrInvalidArchiveKey
	}
	normalized, err := normalizeArchiveKey(key)
	if err != nil {
		return ArchiveRecord{}, err
	}
	data = append([]byte(nil), data...)
	record := ArchiveRecord{
		Key:       normalized,
		Size:      int64(len(data)),
		SHA256:    sha256Hex(data),
		Meta:      cloneArchiveMeta(meta),
		UpdatedAt: s.now().UTC(),
	}
	objectMeta, err := encodeArchiveMeta(record)
	if err != nil {
		return ArchiveRecord{}, err
	}
	if err := s.client.PutObject(ctx, normalized, data, objectMeta); err != nil {
		return ArchiveRecord{}, err
	}
	return record, nil
}

func (s *CloudArchiveStore) Get(ctx context.Context, key string) ([]byte, ArchiveRecord, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, ArchiveRecord{}, false, err
	}
	if s == nil || s.client == nil {
		return nil, ArchiveRecord{}, false, ErrInvalidArchiveKey
	}
	normalized, err := normalizeArchiveKey(key)
	if err != nil {
		return nil, ArchiveRecord{}, false, err
	}
	data, meta, ok, err := s.client.GetObject(ctx, normalized)
	if err != nil || !ok {
		return nil, ArchiveRecord{}, ok, err
	}
	record := decodeArchiveMeta(normalized, data, meta)
	if record.SHA256 != "" && record.SHA256 != sha256Hex(data) {
		return nil, ArchiveRecord{}, false, ErrArchiveCorrupt
	}
	return append([]byte(nil), data...), record, true, nil
}

func (s *CloudArchiveStore) Delete(ctx context.Context, key string) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil || s.client == nil {
		return ErrInvalidArchiveKey
	}
	normalized, err := normalizeArchiveKey(key)
	if err != nil {
		return err
	}
	return s.client.DeleteObject(ctx, normalized)
}

func encodeArchiveMeta(record ArchiveRecord) (map[string]string, error) {
	meta := cloneArchiveMeta(record.Meta)
	data, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	if meta == nil {
		meta = make(map[string]string, 1)
	}
	meta[archiveRecordMetaKey] = string(data)
	return meta, nil
}

func decodeArchiveMeta(key string, data []byte, meta map[string]string) ArchiveRecord {
	if raw := meta[archiveRecordMetaKey]; raw != "" {
		var record ArchiveRecord
		if err := json.Unmarshal([]byte(raw), &record); err == nil && record.Key != "" {
			record.Meta = cloneArchiveMeta(record.Meta)
			return record
		}
	}
	return ArchiveRecord{
		Key:       key,
		Size:      int64(len(data)),
		SHA256:    sha256Hex(data),
		Meta:      cloneArchiveMeta(meta),
		UpdatedAt: parseArchiveTime(meta["updated_at"]),
	}
}

func parseArchiveTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	if ts, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return ts.UTC()
	}
	if unixNano, err := strconv.ParseInt(value, 10, 64); err == nil && unixNano > 0 {
		return time.Unix(0, unixNano).UTC()
	}
	return time.Time{}
}
