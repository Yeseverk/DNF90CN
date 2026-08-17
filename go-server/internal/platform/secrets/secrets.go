package secrets

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"
	"sync"
)

var (
	ErrSecretKeyRequired      = errors.New("secret key is required")
	ErrSecretNotFound         = errors.New("secret not found")
	ErrSecretProviderRequired = errors.New("secret provider is required")
)

type Secret struct {
	Key      string            `json:"key"`
	Value    []byte            `json:"-"`
	Version  string            `json:"version,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Provider 必须可并发调用；Value 返回值必须拷贝，避免轮换后调用方读到共享缓冲区。
type Provider interface {
	GetSecret(context.Context, string) (Secret, bool, error)
}

type ProviderFunc func(context.Context, string) (Secret, bool, error)

func (f ProviderFunc) GetSecret(ctx context.Context, key string) (Secret, bool, error) {
	if f == nil {
		return Secret{}, false, ErrSecretProviderRequired
	}
	return f(ctx, key)
}

type MemoryProvider struct {
	mu      sync.RWMutex
	secrets map[string]Secret
}

func NewMemoryProvider(items ...Secret) (*MemoryProvider, error) {
	p := &MemoryProvider{secrets: make(map[string]Secret, len(items))}
	for _, item := range items {
		if err := p.Set(item); err != nil {
			return nil, err
		}
	}
	return p, nil
}

func (p *MemoryProvider) Set(secret Secret) error {
	key := normalizeKey(secret.Key)
	if key == "" {
		return ErrSecretKeyRequired
	}
	secret.Key = key
	secret.Value = append([]byte(nil), secret.Value...)
	secret.Metadata = cloneMap(secret.Metadata)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.secrets == nil {
		p.secrets = make(map[string]Secret)
	}
	p.secrets[key] = secret
	return nil
}

func (p *MemoryProvider) GetSecret(ctx context.Context, key string) (Secret, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return Secret{}, false, err
	}
	key = normalizeKey(key)
	if key == "" {
		return Secret{}, false, ErrSecretKeyRequired
	}
	if p == nil {
		return Secret{}, false, ErrSecretProviderRequired
	}
	p.mu.RLock()
	secret, ok := p.secrets[key]
	p.mu.RUnlock()
	return cloneSecret(secret), ok, nil
}

type EnvProvider struct {
	Prefix string
}

func (p EnvProvider) GetSecret(ctx context.Context, key string) (Secret, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return Secret{}, false, err
	}
	key = normalizeKey(key)
	if key == "" {
		return Secret{}, false, ErrSecretKeyRequired
	}
	envKey := strings.TrimSpace(p.Prefix) + strings.ToUpper(strings.ReplaceAll(key, "-", "_"))
	value, ok := os.LookupEnv(envKey)
	if !ok {
		return Secret{}, false, nil
	}
	value = strings.TrimRight(value, "\r\n")
	return Secret{Key: key, Value: []byte(value), Metadata: map[string]string{"source": "env", "env_key": envKey}}, true, nil
}

func Required(ctx context.Context, provider Provider, key string) (Secret, error) {
	if provider == nil {
		return Secret{}, ErrSecretProviderRequired
	}
	secret, ok, err := provider.GetSecret(ctx, key)
	if err != nil {
		return Secret{}, err
	}
	if !ok {
		return Secret{}, ErrSecretNotFound
	}
	return secret, nil
}

func (p *MemoryProvider) Keys() []string {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	keys := make([]string, 0, len(p.secrets))
	for key := range p.secrets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func normalizeKey(key string) string {
	return strings.TrimSpace(key)
}

func cloneSecret(secret Secret) Secret {
	secret.Value = append([]byte(nil), secret.Value...)
	secret.Metadata = cloneMap(secret.Metadata)
	return secret
}

func cloneMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
