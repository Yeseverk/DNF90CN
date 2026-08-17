package serializer

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"google.golang.org/protobuf/proto"
)

var (
	ErrUnknownSerializer = errors.New("unknown serializer")
	ErrInvalidTarget     = errors.New("invalid serializer target")
)

type Serializer interface {
	Name() string
	Marshal(any) ([]byte, error)
	Unmarshal([]byte, any) error
}

type Registry struct {
	mu          sync.RWMutex
	serializers map[string]Serializer
}

func NewRegistry(serializers ...Serializer) *Registry {
	r := &Registry{serializers: make(map[string]Serializer)}
	for _, serializer := range serializers {
		_ = r.Register(serializer)
	}
	return r
}

func (r *Registry) Register(serializer Serializer) error {
	if serializer == nil {
		return ErrUnknownSerializer
	}
	name := normalizeName(serializer.Name())
	if name == "" {
		return ErrUnknownSerializer
	}
	r.mu.Lock()
	r.serializers[name] = serializer
	r.mu.Unlock()
	return nil
}

func (r *Registry) Get(name string) (Serializer, bool) {
	if r == nil {
		return nil, false
	}
	name = normalizeName(name)
	r.mu.RLock()
	serializer, ok := r.serializers[name]
	r.mu.RUnlock()
	return serializer, ok
}

func (r *Registry) MustGet(name string) (Serializer, error) {
	serializer, ok := r.Get(name)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownSerializer, name)
	}
	return serializer, nil
}

func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	names := make([]string, 0, len(r.serializers))
	for name := range r.serializers {
		names = append(names, name)
	}
	r.mu.RUnlock()
	sort.Strings(names)
	return names
}

type JSON struct{}

func (JSON) Name() string { return "json" }

func (JSON) Marshal(value any) ([]byte, error) {
	return json.Marshal(value)
}

func (JSON) Unmarshal(data []byte, target any) error {
	if target == nil {
		return ErrInvalidTarget
	}
	return json.Unmarshal(data, target)
}

type Raw struct{}

func (Raw) Name() string { return "raw" }

func (Raw) Marshal(value any) ([]byte, error) {
	switch typed := value.(type) {
	case []byte:
		return append([]byte(nil), typed...), nil
	case string:
		return []byte(typed), nil
	default:
		return nil, fmt.Errorf("%w: raw serializer requires []byte or string", ErrInvalidTarget)
	}
}

func (Raw) Unmarshal(data []byte, target any) error {
	switch typed := target.(type) {
	case *[]byte:
		if typed == nil {
			return ErrInvalidTarget
		}
		*typed = append((*typed)[:0], data...)
		return nil
	case *string:
		if typed == nil {
			return ErrInvalidTarget
		}
		*typed = string(data)
		return nil
	default:
		return fmt.Errorf("%w: raw serializer target must be *[]byte or *string", ErrInvalidTarget)
	}
}

type Protobuf struct {
	DiscardUnknown bool
}

func (Protobuf) Name() string { return "protobuf" }

func (Protobuf) Marshal(value any) ([]byte, error) {
	msg, ok := value.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("%w: protobuf serializer requires proto.Message", ErrInvalidTarget)
	}
	return proto.Marshal(msg)
}

func (p Protobuf) Unmarshal(data []byte, target any) error {
	if target == nil {
		return ErrInvalidTarget
	}
	msg, ok := target.(proto.Message)
	if !ok {
		return fmt.Errorf("%w: protobuf serializer target must be proto.Message", ErrInvalidTarget)
	}
	// 协议层已经限制单包大小；这里再限制嵌套深度，避免恶意递归消息拖垮解码栈。
	return proto.UnmarshalOptions{
		DiscardUnknown: p.DiscardUnknown,
		RecursionLimit: 32,
	}.Unmarshal(data, msg)
}

type LenientProtobuf struct{}

func (LenientProtobuf) Name() string { return "protobuf_lenient" }

func (LenientProtobuf) Marshal(value any) ([]byte, error) {
	return (Protobuf{}).Marshal(value)
}

func (LenientProtobuf) Unmarshal(data []byte, target any) error {
	return (Protobuf{DiscardUnknown: true}).Unmarshal(data, target)
}

var DefaultRegistry = NewRegistry(JSON{}, Raw{}, Protobuf{}, LenientProtobuf{})

func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
