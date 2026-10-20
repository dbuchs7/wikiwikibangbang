package item

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The comment overview (ADR-0063) gathers comments across the items one
// may read: on what one watches, one's own, and those that mention one by
// "@username". It is read from the items; nothing is stored for it.

// Why a comment is in the overview.
const (
	FeedWatched  = "watched"
	FeedMine     = "mine"
	FeedMentions = "mentions"
)

// FeedEntry is one comment in the overview, with its item and why it is
// there.
type FeedEntry struct {
	Item    ID
	Key     string
	Title   string
	Space   string
	Comment Comment
	Why     []string
}

// CommentFeed lists the comments on live items the principal may read
// that are on what they watch, by them, or mention username; with only
// set, those of that kind only. Newest first, skipping offset, at most
// limit; more reports whether further ones follow.
func (x *Session) CommentFeed(username, only string, offset, limit int) (out []FeedEntry, more bool) {
	watches := x.s.watches[x.who]
	var all []FeedEntry
	for _, it := range x.s.items {
		if it.Presence != Live || len(it.Comments) == 0 || !x.s.canRead(x.who, it) {
			continue
		}
		_, watchesItem := watches[watchKey(it.ID, "")]
		_, watchesSpace := watches[watchKey("", it.Space)]
		for _, c := range it.Comments {
			if c.Deleted {
				continue
			}
			var why []string
			if watchesItem || watchesSpace {
				why = append(why, FeedWatched)
			}
			if c.Author == x.who {
				why = append(why, FeedMine)
			}
			if Mentions(c.Text, username) {
				why = append(why, FeedMentions)
			}
			if len(why) == 0 || (only != "" && !slices.Contains(why, only)) {
				continue
			}
			all = append(all, FeedEntry{Item: it.ID, Key: it.Key, Title: it.Title, Space: it.Space, Comment: c, Why: why})
		}
	}
	slices.SortFunc(all, func(a, b FeedEntry) int {
		if c := b.Comment.Created.Compare(a.Comment.Created); c != 0 {
			return c
		}
		return strings.Compare(string(b.Comment.ID), string(a.Comment.ID))
	})
	if offset >= len(all) {
		return nil, false
	}
	all = all[offset:]
	if len(all) > limit {
		return all[:limit], true
	}
	return all, false
}

// Mentions reports whether text names username as "@username", ignoring
// case, not followed by a further letter or digit.
func Mentions(text, username string) bool {
	if username == "" {
		return false
	}
	lower, want := strings.ToLower(text), "@"+strings.ToLower(username)
	for i := 0; ; {
		j := strings.Index(lower[i:], want)
		if j < 0 {
			return false
		}
		end := i + j + len(want)
		next, _ := utf8.DecodeRuneInString(lower[end:])
		if end == len(lower) || !(unicode.IsLetter(next) || unicode.IsDigit(next) || next == '_' || next == '-') {
			return true
		}
		i = end
	}
}
