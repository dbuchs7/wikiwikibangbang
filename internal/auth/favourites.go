package auth

import (
	"fmt"
	"regexp"
	"slices"
)

// maxFavourites bounds how many spaces one user marks as favourites.
const maxFavourites = 200

// favouriteKeyRE is a space key as the item store names spaces.
var favouriteKeyRE = regexp.MustCompile(`^[A-Z][A-Z0-9]*$`)

// Favourites returns the keys of the spaces a user marked as favourites,
// in the order they were marked (ADR-0145). Whether the user still holds
// a role in each is the caller's concern.
func (a *Accounts) Favourites(id UserID) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if u, ok := a.users[id]; ok {
		return slices.Clone(u.favourites)
	}
	return nil
}

// SetFavourite marks a space as one of a user's favourites, or unmarks it.
// Marking a favourite again, or unmarking one that is not, changes nothing.
func (a *Accounts) SetFavourite(id UserID, space string, on bool) error {
	if !favouriteKeyRE.MatchString(space) {
		return fmt.Errorf("%w: space key %q", ErrInvalid, space)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	u, ok := a.users[id]
	if !ok {
		return ErrNotFound
	}
	has := slices.Contains(u.favourites, space)
	if has == on {
		return nil
	}
	next := slices.DeleteFunc(slices.Clone(u.favourites), func(k string) bool { return k == space })
	if on {
		if len(next) >= maxFavourites {
			return fmt.Errorf("%w: at most %d favourites", ErrInvalid, maxFavourites)
		}
		next = append(next, space)
	}
	return a.record(event{Kind: favouritesSet, User: id, Spaces: next})
}
