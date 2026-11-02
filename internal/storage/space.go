package storage

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/dbuchs7/orbithek/internal/diskspace"
)

// ErrNoSpace refuses a change because the data directory's file system is
// almost full (ADR-0150): uploads from the critical level on, every change
// that grows the data below the floor.
var ErrNoSpace = errors.New("the storage is almost full")

// ErrNothingNew refuses a command that makes something new — an item, a
// space, a kanban — from the critical level on (ADR-0163). It is an
// ErrNoSpace, so whatever answers that answers this too.
var ErrNothingNew = fmt.Errorf("%w: no new pages, tasks or rooms can be created until there is room again. Reading, editing existing entries and deleting stay possible", ErrNoSpace)

// nothingNew is the store's check for commands that make something new:
// from the critical level on, they are refused (ADR-0163).
func (db *DB) nothingNew() error {
	if db.Space() >= diskspace.Critical {
		return ErrNothingNew
	}
	return nil
}

// SetSpace records how close the data directory's file system is to full;
// the server measures it (ADR-0150).
func (db *DB) SetSpace(l diskspace.Level) { db.space.Store(int32(l)) }

// Space is how close the data directory's file system is to full, as last
// measured; OK until it is.
func (db *DB) Space() diskspace.Level { return diskspace.Level(db.space.Load()) }

// AttachmentBytes is the size of every attachment file kept, the album's
// pictures and their versions included.
func (db *DB) AttachmentBytes() int64 {
	var n int64
	_ = filepath.WalkDir(filepath.Join(db.dir, AttachmentsDir), func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			n += info.Size()
		}
		return nil
	})
	return n
}
