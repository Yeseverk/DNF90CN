package storageobject

import (
	"context"
	"errors"
	"strings"
)

var ErrPermissionDenied = errors.New("storage object permission denied")

type Subject struct {
	UserID string `json:"user_id,omitempty"`
	Admin  bool   `json:"admin,omitempty"`
	System bool   `json:"system,omitempty"`
}

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) Close() error {
	if s == nil || s.store == nil {
		return nil
	}
	if closer, ok := s.store.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

func (s *Service) Write(ctx context.Context, subject Subject, request WriteRequest) (Object, error) {
	if s == nil || s.store == nil {
		return Object{}, ErrObjectNotFound
	}
	key, err := normalizeKey(request.Object.Key)
	if err != nil {
		return Object{}, err
	}
	current, ok, err := s.store.Read(ctx, key)
	if err != nil {
		return Object{}, err
	}
	guard := request.Object
	if ok {
		guard = current
	}
	if err := AuthorizeWrite(subject, guard); err != nil {
		return Object{}, err
	}
	return s.store.Write(ctx, request)
}

func (s *Service) BatchWrite(ctx context.Context, subject Subject, request BatchWriteRequest) BatchWriteResult {
	result := BatchWriteResult{Results: make([]BatchObjectResult, 0, len(request.Writes))}
	for _, write := range request.Writes {
		object, err := s.Write(ctx, subject, write)
		item := BatchObjectResult{Object: object, Found: err == nil}
		if err != nil {
			item.Error = err.Error()
		}
		result.Results = append(result.Results, item)
	}
	return result
}

func (s *Service) Read(ctx context.Context, subject Subject, key Key) (Object, bool, error) {
	if s == nil || s.store == nil {
		return Object{}, false, nil
	}
	object, ok, err := s.store.Read(ctx, key)
	if err != nil || !ok {
		return object, ok, err
	}
	if err := AuthorizeRead(subject, object); err != nil {
		return Object{}, false, err
	}
	return object, true, nil
}

func (s *Service) BatchRead(ctx context.Context, subject Subject, request BatchReadRequest) BatchReadResult {
	result := BatchReadResult{Results: make([]BatchObjectResult, 0, len(request.Keys))}
	for _, key := range request.Keys {
		object, ok, err := s.Read(ctx, subject, key)
		item := BatchObjectResult{Object: object, Found: ok}
		if err != nil {
			item.Error = err.Error()
		}
		result.Results = append(result.Results, item)
	}
	return result
}

func (s *Service) Delete(ctx context.Context, subject Subject, key Key, ifMatchVersion string) error {
	if s == nil || s.store == nil {
		return ErrObjectNotFound
	}
	object, ok, err := s.store.Read(ctx, key)
	if err != nil {
		return err
	}
	if !ok {
		return ErrObjectNotFound
	}
	if err := AuthorizeWrite(subject, object); err != nil {
		return err
	}
	return s.store.Delete(ctx, key, ifMatchVersion)
}

func (s *Service) BatchDelete(ctx context.Context, subject Subject, request BatchDeleteRequest) BatchDeleteResults {
	result := BatchDeleteResults{Results: make([]BatchDeleteResult, 0, len(request.Deletes))}
	for _, deletion := range request.Deletes {
		err := s.Delete(ctx, subject, deletion.Key, deletion.IfMatchVersion)
		item := BatchDeleteResult{Key: deletion.Key, Deleted: err == nil}
		if err != nil {
			item.Error = err.Error()
		}
		result.Results = append(result.Results, item)
	}
	return result
}

func (s *Service) List(ctx context.Context, subject Subject, request ListRequest) (ListResult, error) {
	if s == nil || s.store == nil {
		return ListResult{}, nil
	}
	if normalizeSubject(subject).Admin {
		result, err := s.store.List(ctx, request)
		if err != nil {
			return ListResult{}, err
		}
		return result, nil
	}
	limit := normalizeListLimit(request.Limit)
	result := ListResult{Objects: make([]Object, 0, limit)}
	scan := request
	scan.Limit = limit
	cursor := strings.TrimSpace(request.Cursor)
	for {
		scan.Cursor = cursor
		page, err := s.store.List(ctx, scan)
		if err != nil {
			return ListResult{}, err
		}
		for _, object := range page.Objects {
			if !CanRead(subject, object) {
				continue
			}
			result.Objects = append(result.Objects, object)
			if len(result.Objects) >= limit {
				result.NextCursor = objectID(object.Key)
				return result, nil
			}
		}
		if strings.TrimSpace(page.NextCursor) == "" {
			return result, nil
		}
		cursor = page.NextCursor
	}
}

func AuthorizeRead(subject Subject, object Object) error {
	if CanRead(subject, object) {
		return nil
	}
	return ErrPermissionDenied
}

func AuthorizeWrite(subject Subject, object Object) error {
	if CanWrite(subject, object) {
		return nil
	}
	return ErrPermissionDenied
}

func CanRead(subject Subject, object Object) bool {
	subject = normalizeSubject(subject)
	if subject.Admin {
		return true
	}
	switch normalizePermission(object.PermissionRead) {
	case PermissionPublic:
		return true
	case PermissionOwner:
		if subject.System {
			return object.OwnerKind() == OwnerKindSystem
		}
		return ownsObject(subject, object)
	default:
		return false
	}
}

func CanWrite(subject Subject, object Object) bool {
	subject = normalizeSubject(subject)
	if subject.Admin {
		return true
	}
	switch normalizePermission(object.PermissionWrite) {
	case PermissionPublic:
		return subject.UserID != "" || subject.System
	case PermissionOwner:
		if subject.System {
			return object.OwnerKind() == OwnerKindSystem
		}
		return ownsObject(subject, object)
	default:
		return false
	}
}

func ownsObject(subject Subject, object Object) bool {
	owner := strings.TrimSpace(object.Key.UserID)
	return subject.UserID != "" && owner != "" && subject.UserID == owner
}

func normalizeSubject(subject Subject) Subject {
	subject.UserID = strings.TrimSpace(subject.UserID)
	return subject
}
