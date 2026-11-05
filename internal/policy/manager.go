package policy

import (
	"rate-limiter/internal/limiter"
)

type Policy struct {
	Limiter   limiter.Limiter
	Config    limiter.Config
	Algorithm string
	Tier      string
}

type Manager struct {
	policies      map[string]Policy
	defaultPolicy Policy
}

func NewManager(defaultPolicy Policy, policies map[string]Policy) *Manager {
	return &Manager{
		policies:      policies,
		defaultPolicy: defaultPolicy,
	}
}

// Get returns the policy assigned to an API key.
// Unknown API keys receive the default policy.
func (m *Manager) Get(apiKey string) Policy {
	p, exists := m.policies[apiKey]
	if !exists {
		return m.defaultPolicy
	}

	return p
}
