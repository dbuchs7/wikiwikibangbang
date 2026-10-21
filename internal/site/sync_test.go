package site

import (
	"errors"
	"fmt"
	"slices"
	"testing"
)

func TestOpenDataSettingsAreKeptAndReplayed(t *testing.T) {
	var records [][]byte
	j := func(r []byte) error { records = append(records, r); return nil }
	s, _ := New(nil, j)
	if got := s.OpenData(); got.Enabled || got.IntervalHours != 24 || !slices.Contains(got.Formats, "WMS") {
		t.Fatalf("default %+v", got)
	}
	o := DefaultOpenData
	o.Enabled, o.Space, o.Formats, o.Organizations = true, " API ", []string{"api", " wms", "API", ""}, []string{"BFS", "bfs", "swisstopo"}
	kept, err := s.SetOpenData("admin", o)
	if err != nil {
		t.Fatal(err)
	}
	if kept.Space != "API" || !slices.Equal(kept.Formats, []string{"API", "WMS"}) || !slices.Equal(kept.Organizations, []string{"bfs", "swisstopo"}) {
		t.Fatalf("normalized %+v", kept)
	}
	r, err := New(records, j)
	if err != nil || !slices.Equal(r.OpenData().Formats, kept.Formats) || r.OpenData().Space != "API" {
		t.Fatalf("replayed %+v %v", r.OpenData(), err)
	}
	if _, err := New([][]byte{[]byte(`{"v":1,"kind":"OpenDataSet"}`)}, j); err == nil {
		t.Error("an open data event without settings replayed")
	}
	for name, change := range map[string]func(*OpenData){
		"no space":     func(o *OpenData) { o.Enabled, o.Space = true, "" },
		"no formats":   func(o *OpenData) { o.Formats = nil },
		"a bad format": func(o *OpenData) { o.Formats = []string{"(API)"} },
		"a bad org":    func(o *OpenData) { o.Organizations = []string{"A B"} },
		"too many orgs": func(o *OpenData) {
			for i := range 201 {
				o.Organizations = append(o.Organizations, fmt.Sprintf("org-%d", i))
			}
		},
		"too many formats": func(o *OpenData) {
			for i := range 61 {
				o.Formats = append(o.Formats, fmt.Sprintf("F%d", i))
			}
		},
		"an interval of 0": func(o *OpenData) { o.IntervalHours = 0 },
		"ftp":              func(o *OpenData) { o.BaseURL = "ftp://x.example" },
		"a password":       func(o *OpenData) { o.PortalURL = "https://u:p@x.example/" },
	} {
		bad := DefaultOpenData
		bad.Formats = slices.Clone(bad.Formats)
		change(&bad)
		if _, err := s.SetOpenData("admin", bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
