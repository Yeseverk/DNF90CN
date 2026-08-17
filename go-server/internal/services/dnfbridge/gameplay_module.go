package dnfbridge

import (
	"errors"
	"fmt"

	dnfproto "longheng.io/server/internal/modules/dnf/protocol"
)

var (
	errGameplayModuleNameRequired = errors.New("gameplay module name is required")
	errGameplayModuleDuplicate    = errors.New("gameplay module route is duplicated")
)

type gameplayRequest struct {
	Opcode         uint16
	Classification byte
	Body           []byte
}

type gameplayHandler func(*Service, *gameSession, gameplayRequest) error
type gameplayLegacyNormalizer func([]byte) []byte

type gameplayModuleDefinition struct {
	Name              string
	LegacyHandlers    map[uint16]gameplayHandler
	UpperHandlers     map[uint16]gameplayHandler
	LegacyNormalizers map[uint16]gameplayLegacyNormalizer
}

type registeredGameplayHandler struct {
	module  string
	handler gameplayHandler
}

type registeredGameplayNormalizer struct {
	module    string
	normalize gameplayLegacyNormalizer
}

type gameplayModuleRegistry struct {
	names             []string
	legacyHandlers    map[uint16]registeredGameplayHandler
	upperHandlers     map[uint16]registeredGameplayHandler
	legacyNormalizers map[uint16]registeredGameplayNormalizer
}

func newGameplayModuleRegistry(modules ...gameplayModuleDefinition) (*gameplayModuleRegistry, error) {
	registry := &gameplayModuleRegistry{
		legacyHandlers:    make(map[uint16]registeredGameplayHandler),
		upperHandlers:     make(map[uint16]registeredGameplayHandler),
		legacyNormalizers: make(map[uint16]registeredGameplayNormalizer),
	}
	seenNames := make(map[string]struct{}, len(modules))
	for _, module := range modules {
		if module.Name == "" {
			return nil, errGameplayModuleNameRequired
		}
		if _, exists := seenNames[module.Name]; exists {
			return nil, fmt.Errorf("%w: module=%s", errGameplayModuleDuplicate, module.Name)
		}
		seenNames[module.Name] = struct{}{}
		registry.names = append(registry.names, module.Name)
		for opcode, handler := range module.LegacyHandlers {
			if handler == nil {
				continue
			}
			if previous, exists := registry.legacyHandlers[opcode]; exists {
				return nil, fmt.Errorf(
					"%w: transport=legacy opcode=%d modules=%s,%s",
					errGameplayModuleDuplicate,
					opcode,
					previous.module,
					module.Name,
				)
			}
			registry.legacyHandlers[opcode] = registeredGameplayHandler{module: module.Name, handler: handler}
		}
		for opcode, handler := range module.UpperHandlers {
			if handler == nil {
				continue
			}
			if previous, exists := registry.upperHandlers[opcode]; exists {
				return nil, fmt.Errorf(
					"%w: transport=upper opcode=%d modules=%s,%s",
					errGameplayModuleDuplicate,
					opcode,
					previous.module,
					module.Name,
				)
			}
			registry.upperHandlers[opcode] = registeredGameplayHandler{module: module.Name, handler: handler}
		}
		for opcode, normalize := range module.LegacyNormalizers {
			if normalize == nil {
				continue
			}
			if previous, exists := registry.legacyNormalizers[opcode]; exists {
				return nil, fmt.Errorf(
					"%w: transport=legacy-normalizer opcode=%d modules=%s,%s",
					errGameplayModuleDuplicate,
					opcode,
					previous.module,
					module.Name,
				)
			}
			registry.legacyNormalizers[opcode] = registeredGameplayNormalizer{module: module.Name, normalize: normalize}
		}
	}
	return registry, nil
}

func mustGameplayModuleRegistry(modules ...gameplayModuleDefinition) *gameplayModuleRegistry {
	registry, err := newGameplayModuleRegistry(modules...)
	if err != nil {
		panic(err)
	}
	return registry
}

func (r *gameplayModuleRegistry) DispatchLegacy(
	service *Service,
	session *gameSession,
	opcode uint16,
	body []byte,
) (bool, error) {
	if r == nil {
		return false, nil
	}
	route, ok := r.legacyHandlers[opcode]
	if !ok {
		return false, nil
	}
	return true, route.handler(service, session, gameplayRequest{Opcode: opcode, Body: body})
}

func (r *gameplayModuleRegistry) DispatchUpper(
	service *Service,
	session *gameSession,
	opcode uint16,
	classification byte,
	body []byte,
) (bool, error) {
	if r == nil {
		return false, nil
	}
	route, ok := r.upperHandlers[opcode]
	if !ok {
		return false, nil
	}
	return true, route.handler(service, session, gameplayRequest{
		Opcode:         opcode,
		Classification: classification,
		Body:           body,
	})
}

func (r *gameplayModuleRegistry) NormalizeLegacy(opcode uint16, body []byte) ([]byte, bool) {
	if r == nil {
		return body, false
	}
	route, ok := r.legacyNormalizers[opcode]
	if !ok {
		return body, false
	}
	return route.normalize(body), true
}

func requireDefaultGameplayClassification(
	service *Service,
	session *gameSession,
	request gameplayRequest,
	event string,
	reason string,
) bool {
	if request.Classification == dnfproto.DefaultChannelClassification {
		return true
	}
	service.logGameEvent(session, event,
		"classification", request.Classification,
		"body_len", len(request.Body),
		"reason", reason)
	return false
}

func defaultClassGameplayHandler(
	event string,
	reason string,
	handler func(*Service, *gameSession, []byte) error,
) gameplayHandler {
	return func(service *Service, session *gameSession, request gameplayRequest) error {
		if !requireDefaultGameplayClassification(service, session, request, event, reason) {
			return nil
		}
		return handler(service, session, request.Body)
	}
}
