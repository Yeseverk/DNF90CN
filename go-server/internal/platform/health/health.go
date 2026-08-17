package health

import (
	"net/http"
	"sort"
	"sync"
	"time"

	"longheng.io/server/internal/platform/httpx"
)

type State string

const (
	StateUnknown  State = "unknown"
	StateStarting State = "starting"
	StateReady    State = "ready"
	StateStopping State = "stopping"
	StateStopped  State = "stopped"
	StateDegraded State = "degraded"
)

type ComponentStatus struct {
	Name      string    `json:"name"`
	State     State     `json:"state"`
	Detail    string    `json:"detail,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Snapshot struct {
	Service     string            `json:"service"`
	State       State             `json:"state"`
	Detail      string            `json:"detail,omitempty"`
	GeneratedAt time.Time         `json:"generated_at"`
	Startup     []ComponentStatus `json:"startup,omitempty"`
	Components  []ComponentStatus `json:"components"`
}

type Service struct {
	service string

	mu         sync.RWMutex
	state      State
	detail     string
	startup    map[string]ComponentStatus
	components map[string]ComponentStatus
}

func New(service string) *Service {
	return &Service{
		service:    service,
		state:      StateStarting,
		startup:    make(map[string]ComponentStatus),
		components: make(map[string]ComponentStatus),
	}
}

func (s *Service) SetServiceState(state State, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = state
	s.detail = detail
}

func (s *Service) SetComponent(name string, state State, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.components[name] = ComponentStatus{
		Name:      name,
		State:     state,
		Detail:    detail,
		UpdatedAt: time.Now().UTC(),
	}
}

func (s *Service) SetStartupStep(name string, state State, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.startup[name] = ComponentStatus{
		Name:      name,
		State:     state,
		Detail:    detail,
		UpdatedAt: time.Now().UTC(),
	}
}

func (s *Service) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	startup := make([]ComponentStatus, 0, len(s.startup))
	for _, step := range s.startup {
		startup = append(startup, step)
	}
	sort.Slice(startup, func(i, j int) bool {
		return startup[i].Name < startup[j].Name
	})

	components := make([]ComponentStatus, 0, len(s.components))
	for _, component := range s.components {
		components = append(components, component)
	}
	sort.Slice(components, func(i, j int) bool {
		return components[i].Name < components[j].Name
	})

	return Snapshot{
		Service:     s.service,
		State:       s.state,
		Detail:      s.detail,
		GeneratedAt: time.Now().UTC(),
		Startup:     startup,
		Components:  components,
	}
}

func (s *Service) LiveHandler(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"service": s.service,
		"alive":   true,
		"time":    time.Now().UTC(),
	})
}

func (s *Service) ReadyHandler(w http.ResponseWriter, _ *http.Request) {
	snapshot := s.Snapshot()
	if !snapshotReady(snapshot) {
		httpx.WriteJSON(w, http.StatusServiceUnavailable, snapshot)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, snapshot)
}

func (s *Service) DebugHandler(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, s.Snapshot())
}

func snapshotReady(snapshot Snapshot) bool {
	if snapshot.State != StateReady {
		return false
	}
	for _, component := range snapshot.Components {
		if component.State != StateReady {
			return false
		}
	}
	return true
}
