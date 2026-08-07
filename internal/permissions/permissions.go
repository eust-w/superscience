package permissions

import (
	"errors"
	"sync"
	"time"
)

type Decision string

const (
	DecisionAllowOnce   Decision = "once"
	DecisionAllowAlways Decision = "always"
	DecisionReject      Decision = "reject"
)

type Request struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Type      string `json:"type"`
	Pattern   string `json:"pattern"`
	Message   string `json:"message"`
	Status    string `json:"status"`
	CreatedAt int64  `json:"createdAt"`
}

type Manager struct {
	mu       sync.Mutex
	pending  map[string]Request
	once     map[string]bool
	always   map[string]bool
	rejected map[string]bool
	nextID   int
}

func NewManager() *Manager {
	return &Manager{
		pending:  map[string]Request{},
		once:     map[string]bool{},
		always:   map[string]bool{},
		rejected: map[string]bool{},
	}
}

func (m *Manager) Ask(request Request) Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	request.ID = "permission_" + time.Now().Format("150405000") + "_" + string(rune('a'+m.nextID%26))
	request.Status = "pending"
	request.CreatedAt = time.Now().UnixMilli()
	m.pending[request.ID] = request
	return request
}

func (m *Manager) Pending() []Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]Request, 0, len(m.pending))
	for _, request := range m.pending {
		result = append(result, request)
	}
	return result
}

func (m *Manager) Respond(id string, decision Decision) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	request, ok := m.pending[id]
	if !ok {
		return errors.New("permission not found")
	}
	delete(m.pending, id)
	key := permissionKey(request.SessionID, request.Type, request.Pattern)
	switch decision {
	case DecisionAllowOnce:
		m.once[key] = true
	case DecisionAllowAlways:
		m.always[key] = true
	case DecisionReject:
		m.rejected[key] = true
	default:
		return errors.New("unknown permission decision")
	}
	return nil
}

func (m *Manager) IsAllowed(sessionID, typ, pattern string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := permissionKey(sessionID, typ, pattern)
	if m.rejected[key] {
		return false
	}
	if m.always[key] {
		return true
	}
	if m.once[key] {
		delete(m.once, key)
		return true
	}
	return false
}

func permissionKey(sessionID, typ, pattern string) string {
	return sessionID + "\x00" + typ + "\x00" + pattern
}
