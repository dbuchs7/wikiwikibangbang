package site

// AlbumRetention is the album's default rule for the older versions of a
// picture (ADR-0104): keep the Keep newest versions and those younger than
// Days days; 0 sets no limit of its kind. A picture's own rule takes
// precedence. With Automatic, the server applies the rules every day;
// without, only someone cleaning a picture up in the album does.
type AlbumRetention struct {
	Automatic bool `json:"automatic"`
	Keep      int  `json:"keep"`
	Days      int  `json:"days"`
}

// AlbumRetention returns the album's default rule; until an administrator
// sets one it keeps every version and runs nothing on its own.
func (s *Settings) AlbumRetention() AlbumRetention {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.albumRetention
}

// SetAlbumRetention replaces the album's default rule. The caller checks
// its limits, which are the item model's (ADR-0104).
func (s *Settings) SetAlbumRetention(actor string, r AlbumRetention) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.albumRetention == r {
		return nil
	}
	return s.record(event{Kind: albumRetentionSet, Actor: actor, AlbumRetention: &r})
}
