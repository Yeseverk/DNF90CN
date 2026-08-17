package module

import (
	"errors"
	"sort"
	"strings"
	"sync"
)

var (
	ErrModuleNameRequired    = errors.New("module name is required")
	ErrModuleFactoryRequired = errors.New("module factory is required")
	ErrModuleNotFound        = errors.New("module not found")
)

type Descriptor struct {
	Name     string            `json:"name"`
	Version  string            `json:"version,omitempty"`
	Kind     string            `json:"kind,omitempty"`
	Critical bool              `json:"critical,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type Factory func() (Module, error)

type Registry struct {
	mu      sync.RWMutex
	entries map[string]registeredModule
}

type registeredModule struct {
	descriptor Descriptor
	factory    Factory
}

func NewRegistry() *Registry {
	return &Registry{entries: make(map[string]registeredModule)}
}

func (r *Registry) Register(descriptor Descriptor, factory Factory) error {
	name := strings.TrimSpace(descriptor.Name)
	if name == "" {
		return ErrModuleNameRequired
	}
	if factory == nil {
		return ErrModuleFactoryRequired
	}
	descriptor.Name = name
	descriptor.Metadata = cloneDescMeta(descriptor.Metadata)
	if r == nil {
		return ErrModuleFactoryRequired
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.entries == nil {
		r.entries = make(map[string]registeredModule)
	}
	r.entries[name] = registeredModule{descriptor: descriptor, factory: factory}
	return nil
}

func (r *Registry) Create(name string) (Module, Descriptor, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, Descriptor{}, ErrModuleNameRequired
	}
	if r == nil {
		return nil, Descriptor{}, ErrModuleNotFound
	}
	r.mu.RLock()
	entry, ok := r.entries[name]
	r.mu.RUnlock()
	if !ok {
		return nil, Descriptor{}, ErrModuleNotFound
	}
	mod, err := entry.factory()
	if err != nil {
		return nil, Descriptor{}, err
	}
	if mod == nil {
		return nil, Descriptor{}, ErrModuleFactoryRequired
	}
	return mod, cloneDescriptor(entry.descriptor), nil
}

func (r *Registry) List() []Descriptor {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	out := make([]Descriptor, 0, len(r.entries))
	for _, entry := range r.entries {
		out = append(out, cloneDescriptor(entry.descriptor))
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func cloneDescriptor(in Descriptor) Descriptor {
	in.Metadata = cloneDescMeta(in.Metadata)
	return in
}

func cloneDescMeta(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
