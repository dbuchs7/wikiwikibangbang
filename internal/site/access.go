package site

// Access is how people come to an account (ADR-0061).
type Access struct {
	// AccountRequests lets people ask for an account at the sign-in page;
	// an administrator approves or rejects each request.
	AccountRequests bool `json:"accountRequests"`
}

// Access returns the access settings.
func (s *Settings) Access() Access {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.access
}

// SetAccess replaces the access settings.
func (s *Settings) SetAccess(actor string, a Access) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.access == a {
		return nil
	}
	return s.record(event{Kind: accessSet, Actor: actor, Access: &a})
}
