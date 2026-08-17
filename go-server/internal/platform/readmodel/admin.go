package readmodel

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
)

var ErrAdminBadRequest = errors.New("read model admin bad request")

type AdminError struct {
	Kind    error
	Message string
}

func (e AdminError) Error() string {
	return e.Message
}

func (e AdminError) Unwrap() error {
	return e.Kind
}

func BadRequest(message string) error {
	return AdminError{Kind: ErrAdminBadRequest, Message: strings.TrimSpace(message)}
}

func IsBadRequest(err error) bool {
	return errors.Is(err, ErrAdminBadRequest)
}

type AdminSearchFunc func(context.Context, url.Values) (any, error)
type AdminRebuildFunc func(context.Context, url.Values) (any, error)

type AdminModel struct {
	Name    string
	Search  AdminSearchFunc
	Rebuild AdminRebuildFunc
}

type AdminRegistry struct {
	mu     sync.RWMutex
	models map[string]AdminModel
}

func NewAdminRegistry() *AdminRegistry {
	return &AdminRegistry{
		models: make(map[string]AdminModel),
	}
}

func (r *AdminRegistry) Register(model AdminModel) {
	if r == nil {
		return
	}
	name := normAdminModel(model.Name)
	if name == "" {
		return
	}
	model.Name = name
	r.mu.Lock()
	r.models[name] = model
	r.mu.Unlock()
}

func (r *AdminRegistry) Search(ctx context.Context, name string, values url.Values) (any, bool, error) {
	model, ok := r.model(name)
	if !ok || model.Search == nil {
		return nil, ok, nil
	}
	out, err := model.Search(ctx, values)
	return out, true, err
}

func (r *AdminRegistry) Rebuild(ctx context.Context, name string, values url.Values) (any, bool, error) {
	model, ok := r.model(name)
	if !ok || model.Rebuild == nil {
		return nil, ok, nil
	}
	out, err := model.Rebuild(ctx, values)
	return out, true, err
}

func (r *AdminRegistry) model(name string) (AdminModel, bool) {
	if r == nil {
		return AdminModel{}, false
	}
	name = normAdminModel(name)
	r.mu.RLock()
	model, ok := r.models[name]
	r.mu.RUnlock()
	return model, ok
}

func normAdminModel(name string) string {
	name = strings.TrimSpace(name)
	name = strings.Trim(name, "/")
	return name
}
