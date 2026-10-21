package site

import "fmt"

// MaxBackupVersions is the most backup versions an administrator may keep
// (ADR-0073): a version is a full backup with the incremental backups
// that build on it.
const MaxBackupVersions = 10

// Backup is the schedule of automatic backups (ADR-0073). Where they go is
// set here; the passphrase and the credentials of the destination are
// not, they come from the server's environment.
type Backup struct {
	Enabled bool `json:"enabled"`
	// Destination is where backups are stored: a directory of the server
	// for now; the scheme of an address names its kind.
	Destination string `json:"destination"`
	// FullDays is the number of days between full backups.
	FullDays int `json:"fullDays"`
	// IncrementalDays is the number of days between incremental backups;
	// 0 takes none.
	IncrementalDays int `json:"incrementalDays"`
	// Keep is the number of versions kept; an older one is deleted with
	// its incremental backups once a newer full backup is stored.
	Keep int `json:"keep"`
}

// DefaultBackup is the schedule until an administrator sets one: a full
// backup every 14 days, an incremental one every day, three versions.
var DefaultBackup = Backup{FullDays: 14, IncrementalDays: 1, Keep: 3}

// Validate checks the schedule; whether the destination can be reached is
// the caller's concern.
func (b Backup) Validate() error {
	switch {
	case b.FullDays < 1 || b.FullDays > 365:
		return fmt.Errorf("%w: a full backup is taken every 1 to 365 days", ErrInvalid)
	case b.IncrementalDays < 0 || b.IncrementalDays >= b.FullDays && b.IncrementalDays != 0:
		return fmt.Errorf("%w: incremental backups are taken more often than full ones, or not at all (0)", ErrInvalid)
	case b.Keep < 1 || b.Keep > MaxBackupVersions:
		return fmt.Errorf("%w: 1 to %d versions are kept", ErrInvalid, MaxBackupVersions)
	case len(b.Destination) > 1024:
		return fmt.Errorf("%w: the destination is too long", ErrInvalid)
	case b.Enabled && b.Destination == "":
		return fmt.Errorf("%w: automatic backups need a destination", ErrInvalid)
	}
	return nil
}

// Backup returns the schedule of automatic backups.
func (s *Settings) Backup() Backup {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.backup == nil {
		return DefaultBackup
	}
	return *s.backup
}

// SetBackup replaces the schedule of automatic backups.
func (s *Settings) SetBackup(actor string, b Backup) error {
	if err := b.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.backup != nil && *s.backup == b {
		return nil
	}
	return s.record(event{Kind: backupSet, Actor: actor, Backup: &b})
}
