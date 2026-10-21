package backup

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"time"
)

// The file name of a stored backup tells which chain it belongs to and
// where in it (ADR-0073), so a destination can be read without decrypting
// anything: wikiwikibangbang-<when>-<chain>-<seq>-full|inc.wwbb.
var namePattern = regexp.MustCompile(`^wikiwikibangbang-(\d{8}T\d{6}Z)-([0-9a-f]{16})-(\d{3,6})-(full|inc)\.wwbb$`)

const nameTime = "20060102T150405Z"

// Name is what a stored backup's file name tells.
type Name struct {
	File  string
	At    time.Time
	Chain string
	Seq   int
}

// FileName is the name a backup is stored under.
func FileName(at time.Time, chain string, seq int) string {
	kind := "inc"
	if seq == 0 {
		kind = "full"
	}
	return fmt.Sprintf("wikiwikibangbang-%s-%s-%03d-%s.wwbb", at.UTC().Format(nameTime), chain, seq, kind)
}

// ParseName reads a stored backup's file name; ok is false for any other
// file.
func ParseName(file string) (n Name, ok bool) {
	p := namePattern.FindStringSubmatch(file)
	if p == nil {
		return n, false
	}
	at, err := time.Parse(nameTime, p[1])
	if err != nil {
		return n, false
	}
	seq, _ := strconv.Atoi(p[3])
	if (seq == 0) != (p[4] == "full") {
		return n, false
	}
	return Name{File: file, At: at, Chain: p[2], Seq: seq}, true
}

// Version is a full backup and the incremental backups stored after it,
// in order.
type Version struct {
	Chain   string
	Backups []Name
}

// Complete tells whether every backup of the version, from the full one
// on, is there, so its last restores.
func (v Version) Complete() bool {
	for i, b := range v.Backups {
		if b.Seq != i {
			return false
		}
	}
	return len(v.Backups) > 0
}

// Versions groups the backups among files by chain, oldest first; files
// that are not backups are left out.
func Versions(files []string) []Version {
	byChain := map[string]*Version{}
	var out []*Version
	for _, f := range files {
		n, ok := ParseName(f)
		if !ok {
			continue
		}
		v := byChain[n.Chain]
		if v == nil {
			v = &Version{Chain: n.Chain}
			byChain[n.Chain] = v
			out = append(out, v)
		}
		v.Backups = append(v.Backups, n)
	}
	for _, v := range out {
		slices.SortFunc(v.Backups, func(a, b Name) int { return a.Seq - b.Seq })
	}
	slices.SortFunc(out, func(a, b *Version) int { return a.Backups[0].At.Compare(b.Backups[0].At) })
	res := make([]Version, len(out))
	for i, v := range out {
		res[i] = *v
	}
	return res
}
