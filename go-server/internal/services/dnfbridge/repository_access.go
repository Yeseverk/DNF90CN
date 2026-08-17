package dnfbridge

import (
	"strings"

	dnfrepo "longheng.io/server/internal/modules/dnf/repository"
)

func (s *Service) repositoryGroup() (dnfrepo.Group, bool) {
	if s == nil || s.repositoryProvider == nil {
		return dnfrepo.Group{}, false
	}
	return s.repositoryProvider()
}

func (s *Service) accountID() string {
	if s != nil {
		if value := strings.TrimSpace(s.options.accountID); value != "" {
			return value
		}
		prefix := strings.TrimSpace(s.options.accountPrefix)
		if prefix == "" {
			prefix = defaultAccountPrefix
		}
		return prefix + "1"
	}
	return defaultAccountPrefix + "1"
}

func (s *Service) accountIDForSession(sessions ...*gameSession) string {
	if len(sessions) > 0 && sessions[0] != nil {
		if value := strings.TrimSpace(sessions[0].accountID); value != "" {
			return value
		}
		if value, found := s.registeredClientAccount(sessions[0].clientPID); found {
			return value
		}
	}
	return s.accountID()
}
