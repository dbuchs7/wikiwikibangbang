package site

import (
	"fmt"

	"github.com/dbuchs7/wikiwikibangbang/internal/diskspace"
)

// DiskSpace returns the thresholds of free space; until an administrator
// sets others, the defaults (ADR-0150).
func (s *Settings) DiskSpace() diskspace.Thresholds {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.diskSpace == nil {
		return diskspace.Defaults
	}
	return *s.diskSpace
}

// SetDiskSpace replaces the thresholds of free space.
func (s *Settings) SetDiskSpace(actor string, t diskspace.Thresholds) error {
	if err := t.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.diskSpace != nil && *s.diskSpace == t {
		return nil
	}
	return s.record(event{Kind: diskSpaceSet, Actor: actor, DiskSpace: &t})
}
