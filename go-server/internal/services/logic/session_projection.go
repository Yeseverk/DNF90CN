package logic

import "strings"

func (s *Service) markSessionConnected(accountID, sessionID string) {
	accountID = strings.TrimSpace(accountID)
	sessionID = strings.TrimSpace(sessionID)
	if accountID == "" || sessionID == "" {
		return
	}
	// logic 只认每个账号的最新 session；新连接到达后要清掉同 session 的关闭标记，避免后续包被误判 stale。
	s.sessionMu.Lock()
	s.activeSessions[accountID] = sessionID
	delete(s.closedSessions, sessionKey(accountID, sessionID))
	s.sessionMu.Unlock()
}

func (s *Service) markSessGone(accountID, sessionID string) (bool, bool) {
	accountID = strings.TrimSpace(accountID)
	sessionID = strings.TrimSpace(sessionID)
	if accountID == "" || sessionID == "" {
		return true, false
	}
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	// 旧 session 的断开事件可能晚于新 session 连接事件到达；这种情况只记录关闭，不允许它清掉当前在线投影。
	current, ok := s.activeSessions[accountID]
	if ok && current != sessionID {
		s.closedSessions[sessionKey(accountID, sessionID)] = struct{}{}
		return false, false
	}
	if ok {
		delete(s.activeSessions, accountID)
	}
	s.closedSessions[sessionKey(accountID, sessionID)] = struct{}{}
	return true, ok
}

func (s *Service) clearCurSession(accountID, sessionID string) {
	accountID = strings.TrimSpace(accountID)
	sessionID = strings.TrimSpace(sessionID)
	if accountID == "" || sessionID == "" {
		return
	}
	s.sessionMu.Lock()
	if s.activeSessions[accountID] == sessionID {
		delete(s.activeSessions, accountID)
	}
	s.sessionMu.Unlock()
}

func (s *Service) restoreSessGone(accountID, sessionID string, hadCurrent bool) {
	accountID = strings.TrimSpace(accountID)
	sessionID = strings.TrimSpace(sessionID)
	if accountID == "" || sessionID == "" {
		return
	}
	s.sessionMu.Lock()
	delete(s.closedSessions, sessionKey(accountID, sessionID))
	if hadCurrent {
		s.activeSessions[accountID] = sessionID
	}
	s.sessionMu.Unlock()
}

func (s *Service) isStaleSession(accountID, sessionID string) bool {
	accountID = strings.TrimSpace(accountID)
	sessionID = strings.TrimSpace(sessionID)
	if accountID == "" || sessionID == "" {
		return false
	}
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	// 包处理前做 stale session 检查，防止被踢下线或重连前的旧连接继续修改 Profile。
	current, ok := s.activeSessions[accountID]
	if ok {
		return current != sessionID
	}
	_, closed := s.closedSessions[sessionKey(accountID, sessionID)]
	return closed
}

func (s *Service) resetSessionTracking() {
	s.sessionMu.Lock()
	s.activeSessions = make(map[string]string)
	s.closedSessions = make(map[string]struct{})
	s.sessionMu.Unlock()
}

func sessionKey(accountID, sessionID string) string {
	accountID = strings.TrimSpace(accountID)
	sessionID = strings.TrimSpace(sessionID)
	return accountID + "\x00" + sessionID
}
