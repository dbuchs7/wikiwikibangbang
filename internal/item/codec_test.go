package item

import (
	"errors"
	"flag"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// TestEveryValueTypeSurvivesTheJournal relies on newTestStore's round-trip
// journal: at the end of the test the decoded log must rebuild items whose
// property values have the same Go types as the live ones.
func TestEveryValueTypeSurvivesTheJournal(t *testing.T) {
	s := setup(t)
	noErr(t, s.DefineType("admin", TypeDef{Name: "record", Fields: []FieldDef{
		{Name: "t", Kind: KindText}, {Name: "i", Kind: KindNumber}, {Name: "f", Kind: KindNumber},
		{Name: "b", Kind: KindBool}, {Name: "d", Kind: KindDate}, {Name: "u", Kind: KindUser},
		{Name: "e", Kind: KindEnum, Values: []string{"a", "b"}},
	}}))
	zurich := time.FixedZone("CET", 3600)
	props := Props{"t": "x", "i": 3, "f": 0.1, "b": false, "d": time.Date(2026, 1, 2, 3, 4, 5, 6, zurich), "u": "anna", "e": "b"}
	it := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "record", Title: "r", Properties: props}))
	if d := it.Properties["d"].(time.Time); d.Location() != time.UTC {
		t.Errorf("created with a date in %v, want UTC", d.Location())
	}
	noErr(t, s.SetProperty("anna", it.ID, "d", time.Now()))
	noErr(t, s.SetProperty("anna", it.ID, "i", -7))
	noErr(t, s.SetProperty("anna", it.ID, "f", nil))

	if props["d"].(time.Time).Location() != zurich {
		t.Error("recording changed the caller's property map")
	}
	got := mustItem(t)(s.Get(it.ID))
	if _, isInt := got.Properties["i"].(int); !isInt {
		t.Errorf("i is %T, want int", got.Properties["i"])
	}
	if d := got.Properties["d"].(time.Time); d.Location() != time.UTC {
		t.Errorf("set to a date in %v, want UTC", d.Location())
	}
}

func TestNonFiniteNumbersAreRejected(t *testing.T) {
	s := setup(t)
	task := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "task", Title: "t", Properties: Props{"priority": "low"}}))
	for _, v := range []float64{math.NaN(), math.Inf(1)} {
		if err := s.SetProperty("anna", task.ID, "estimate", v); !errors.Is(err, ErrValidation) {
			t.Errorf("estimate = %v: err = %v, want ErrValidation", v, err)
		}
	}
}

type failingJournal struct{ err error }

func (j failingJournal) Append([]Event) error { return j.err }

func TestAChangeThatIsNotPersistedDoesNotHappen(t *testing.T) {
	boom := errors.New("disk full")
	s := NewStore(WithJournal(failingJournal{boom}))
	err := s.CreateSpace("admin", "PLAT", "Platform")
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the journal's error", err)
	}
	if s.HasSpace("PLAT") || len(s.Log()) != 0 {
		t.Fatal("a change the journal refused became visible")
	}
}

func TestDecodeRejectsWhatItCannotReadExactly(t *testing.T) {
	cases := map[string]string{
		"newer format":       `[{"v":30,"seq":1,"at":"2026-01-01T00:00:00Z","kind":"SpaceCreated"}]`,
		"no format version":  `[{"seq":1,"at":"2026-01-01T00:00:00Z","kind":"SpaceCreated"}]`,
		"unknown field":      `[{"v":1,"seq":1,"at":"2026-01-01T00:00:00Z","kind":"SpaceCreated","colour":"red"}]`,
		"unknown role":       `[{"v":1,"seq":1,"at":"2026-01-01T00:00:00Z","kind":"RoleGranted","role":"owner"}]`,
		"unknown value type": `[{"v":1,"seq":1,"at":"2026-01-01T00:00:00Z","kind":"PropertySet","value":{"t":"x","v":"1"}}]`,
		"bad prop value":     `[{"v":1,"seq":1,"at":"2026-01-01T00:00:00Z","kind":"ItemCreated","props":{"n":{"t":"i","v":"1.5"}}}]`,
		"empty batch":        `[]`,
		"not json":           `{`,
	}
	for name, data := range cases {
		if _, err := DecodeBatch([]byte(data)); err == nil {
			t.Errorf("%s: decoded without error", name)
		}
	}
}

func TestEncodeRejectsWhatItCannotStore(t *testing.T) {
	cases := map[string]Event{
		"unknown role":      {Kind: RoleGranted, Role: 99},
		"unsupported value": {Kind: PropertySet, Value: []string{"x"}},
		"unsupported prop":  {Kind: ItemCreated, Props: Props{"x": struct{}{}}},
		"NaN":               {Kind: PropertySet, Value: math.NaN()},
	}
	for name, e := range cases {
		if _, err := EncodeBatch([]Event{e}); err == nil {
			t.Errorf("%s: encoded without error", name)
		}
	}
}

// fakeSealer seals deterministically, so the golden file can pin the sealed
// layout. Items in shredded have lost their key.
type fakeSealer struct{ shredded map[ID]bool }

func (f fakeSealer) Seal(id ID, pt, aad []byte) ([]byte, error) {
	return append([]byte("sealed("+string(id)+")"), pt...), nil
}

func (f fakeSealer) Open(id ID, ct, aad []byte) ([]byte, error) {
	if f.shredded[id] {
		return nil, ErrKeyShredded
	}
	prefix := "sealed(" + string(id) + ")"
	if !strings.HasPrefix(string(ct), prefix) {
		return nil, errors.New("wrong key")
	}
	return ct[len(prefix):], nil
}

// TestPersistedFormatIsStable pins the format of a log covering every event
// kind, in plaintext and sealed. A failure means old data directories would
// be read differently: change the format only together with CodecVersion
// and a decoder for the old version, then regenerate with
// `go test ./internal/item -run PersistedFormat -update`.
func TestPersistedFormatIsStable(t *testing.T) {
	log := everyKindLog(t)
	rich := richTextLog(t)
	comments := commentLog(t)
	tree := treeLog(t)
	blocks := blockKindsLog(t)
	labels := labelLog(t)
	files := attachmentLog(t)
	watches := watchLog(t)
	inline := inlineCommentLog(t)
	access := revocationLog(t)
	provenance := provenanceLog(t)
	sizes := imageSizeLog(t)
	holds := holdLog(t)
	layout := layoutLog(t)
	kanbans := kanbanLog(t)
	columns := kanbanColumnsLog(t)
	spaces := spaceLog(t)
	names := spaceNameLog(t)
	secrets := secretLog(t)
	erasure := secretErasureLog(t)
	album := albumLog(t)
	catalog := catalogLog(t)
	removal := typeRemovalLog(t)
	video := videoLog(t)
	retention := retentionLog(t)
	tasklist := taskListLog(t)
	lists := listsLog(t)
	diagrams := bpmnLog(t)
	kinds := map[EventKind]bool{}
	for _, e := range slices.Concat(log, rich, comments, tree, blocks, labels, files, watches, inline, access, provenance, sizes, holds, layout, kanbans, spaces, catalog, columns, names, secrets, erasure, album, removal, video, retention, tasklist, lists, diagrams) {
		kinds[e.Kind] = true
	}
	for _, k := range allEventKinds {
		if !kinds[k] {
			t.Errorf("the golden logs have no %s event; extend everyKindLog or commentLog", k)
		}
	}
	cases := []struct {
		name string
		sl   Sealer
		log  []Event
	}{
		{"events.v29.golden.jsonl", nil, log},
		{"events.v29.sealed.golden.jsonl", fakeSealer{}, log},
		{"events.v29.richtext.golden.jsonl", fakeSealer{}, rich},
		{"events.v29.comments.golden.jsonl", fakeSealer{}, comments},
		{"events.v29.tree.golden.jsonl", fakeSealer{}, tree},
		{"events.v29.blocks.golden.jsonl", fakeSealer{}, blocks},
		{"events.v29.labels.golden.jsonl", fakeSealer{}, labels},
		{"events.v29.attachments.golden.jsonl", fakeSealer{}, files},
		{"events.v29.attachments.plain.golden.jsonl", nil, files},
		{"events.v29.watches.golden.jsonl", fakeSealer{}, watches},
		{"events.v29.inline.golden.jsonl", fakeSealer{}, inline},
		{"events.v29.revocations.golden.jsonl", nil, access},
		{"events.v29.provenance.golden.jsonl", fakeSealer{}, provenance},
		{"events.v29.imagesize.golden.jsonl", fakeSealer{}, sizes},
		{"events.v29.dataprotection.golden.jsonl", fakeSealer{}, holds},
		{"events.v29.dataprotection.plain.golden.jsonl", nil, holds},
		{"events.v29.layout.golden.jsonl", fakeSealer{}, layout},
		{"events.v29.kanbans.golden.jsonl", nil, kanbans},
		{"events.v29.spaces.golden.jsonl", nil, spaces},
		{"events.v29.catalog.golden.jsonl", nil, catalog},
		{"events.v29.kanbancolumns.golden.jsonl", nil, columns},
		{"events.v29.spacenames.golden.jsonl", nil, names},
		{"events.v29.secrets.golden.jsonl", fakeSealer{}, secrets},
		{"events.v29.secrets.plain.golden.jsonl", nil, secrets},
		{"events.v29.secreterasure.golden.jsonl", fakeSealer{}, erasure},
		{"events.v29.secreterasure.plain.golden.jsonl", nil, erasure},
		{"events.v29.album.golden.jsonl", fakeSealer{}, album},
		{"events.v29.album.plain.golden.jsonl", nil, album},
		{"events.v29.typeremoval.golden.jsonl", nil, removal},
		{"events.v29.video.golden.jsonl", fakeSealer{}, video},
		{"events.v29.video.plain.golden.jsonl", nil, video},
		{"events.v29.retention.golden.jsonl", fakeSealer{}, retention},
		{"events.v29.retention.plain.golden.jsonl", nil, retention},
		{"events.v29.tasklist.golden.jsonl", fakeSealer{}, tasklist},
		{"events.v29.tasklist.plain.golden.jsonl", nil, tasklist},
		{"events.v29.lists.golden.jsonl", fakeSealer{}, lists},
		{"events.v29.lists.plain.golden.jsonl", nil, lists},
		{"events.v29.bpmn.golden.jsonl", fakeSealer{}, diagrams},
		{"events.v29.bpmn.plain.golden.jsonl", nil, diagrams},
	}
	for _, c := range cases {
		name, sl, log := c.name, c.sl, c.log
		var lines []string
		for _, e := range log {
			data, err := EncodeBatchSealed([]Event{e}, sl)
			if err != nil {
				t.Fatal(err)
			}
			lines = append(lines, string(data))
		}
		got := strings.Join(lines, "\n") + "\n"
		golden := filepath.Join("testdata", name)
		if *update {
			if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("%v (run with -update to create it)", err)
		}
		if got != string(want) {
			t.Fatalf("%s: the persisted format changed; see the comment on this test\n got: %s\nwant: %s", name, got, want)
		}
		assertRebuilds(t, golden, sl, log)
	}
}

// TestOlderVersionsStillReplay: data directories written by earlier codec
// versions — 1 (plaintext, nothing erased), 2 (sealed, erased), 3 (rich
// text), 4 (comments), 5 (sibling order), 6 (block kinds), 7 (labels), 8
// (attachments), 9 (watches), 10 (inline comments), 11 (revocations), 12 (provenance), 13 (image sizes), 14 (data protection), 15 (layout), 16 (kanbans), 17 (spaces), 18 (catalogs), 19 (kanban columns), 20 (space names), 21 (protected passages), 22 (erased passages), 23 (picture labels), 24 (type removal), 25 (videos), 26 (picture retention), 27 (task lists), 28 (lists of figures and tables) — still open and rebuild the same state and history as today's log
// of the same commands, including the content erased by the purges they
// contain.
func TestOlderVersionsStillReplay(t *testing.T) {
	log := everyKindLog(t)
	assertRebuilds(t, filepath.Join("testdata", "events.v1.golden.jsonl"), nil, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v2.golden.jsonl"), nil, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v2.sealed.golden.jsonl"), fakeSealer{}, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v3.golden.jsonl"), nil, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v3.sealed.golden.jsonl"), fakeSealer{}, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v3.richtext.golden.jsonl"), fakeSealer{}, richTextLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v4.golden.jsonl"), nil, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v4.sealed.golden.jsonl"), fakeSealer{}, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v4.richtext.golden.jsonl"), fakeSealer{}, richTextLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v4.comments.golden.jsonl"), fakeSealer{}, commentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v5.golden.jsonl"), nil, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v5.sealed.golden.jsonl"), fakeSealer{}, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v5.richtext.golden.jsonl"), fakeSealer{}, richTextLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v5.comments.golden.jsonl"), fakeSealer{}, commentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v5.tree.golden.jsonl"), fakeSealer{}, treeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v6.golden.jsonl"), nil, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v6.sealed.golden.jsonl"), fakeSealer{}, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v6.richtext.golden.jsonl"), fakeSealer{}, richTextLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v6.comments.golden.jsonl"), fakeSealer{}, commentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v6.tree.golden.jsonl"), fakeSealer{}, treeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v6.blocks.golden.jsonl"), fakeSealer{}, blockKindsLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v7.golden.jsonl"), nil, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v7.sealed.golden.jsonl"), fakeSealer{}, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v7.richtext.golden.jsonl"), fakeSealer{}, richTextLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v7.comments.golden.jsonl"), fakeSealer{}, commentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v7.tree.golden.jsonl"), fakeSealer{}, treeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v7.blocks.golden.jsonl"), fakeSealer{}, blockKindsLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v7.labels.golden.jsonl"), fakeSealer{}, labelLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v8.golden.jsonl"), nil, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v8.sealed.golden.jsonl"), fakeSealer{}, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v8.richtext.golden.jsonl"), fakeSealer{}, richTextLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v8.comments.golden.jsonl"), fakeSealer{}, commentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v8.tree.golden.jsonl"), fakeSealer{}, treeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v8.blocks.golden.jsonl"), fakeSealer{}, blockKindsLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v8.labels.golden.jsonl"), fakeSealer{}, labelLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v8.attachments.golden.jsonl"), fakeSealer{}, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v9.golden.jsonl"), nil, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v9.sealed.golden.jsonl"), fakeSealer{}, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v9.richtext.golden.jsonl"), fakeSealer{}, richTextLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v9.comments.golden.jsonl"), fakeSealer{}, commentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v9.tree.golden.jsonl"), fakeSealer{}, treeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v9.blocks.golden.jsonl"), fakeSealer{}, blockKindsLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v9.labels.golden.jsonl"), fakeSealer{}, labelLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v9.attachments.golden.jsonl"), fakeSealer{}, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v9.attachments.plain.golden.jsonl"), nil, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v9.watches.golden.jsonl"), fakeSealer{}, watchLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v10.golden.jsonl"), nil, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v10.sealed.golden.jsonl"), fakeSealer{}, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v10.richtext.golden.jsonl"), fakeSealer{}, richTextLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v10.comments.golden.jsonl"), fakeSealer{}, commentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v10.tree.golden.jsonl"), fakeSealer{}, treeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v10.blocks.golden.jsonl"), fakeSealer{}, blockKindsLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v10.labels.golden.jsonl"), fakeSealer{}, labelLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v10.attachments.golden.jsonl"), fakeSealer{}, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v10.attachments.plain.golden.jsonl"), nil, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v10.watches.golden.jsonl"), fakeSealer{}, watchLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v10.inline.golden.jsonl"), fakeSealer{}, inlineCommentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v11.golden.jsonl"), nil, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v11.sealed.golden.jsonl"), fakeSealer{}, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v11.richtext.golden.jsonl"), fakeSealer{}, richTextLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v11.comments.golden.jsonl"), fakeSealer{}, commentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v11.tree.golden.jsonl"), fakeSealer{}, treeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v11.blocks.golden.jsonl"), fakeSealer{}, blockKindsLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v11.labels.golden.jsonl"), fakeSealer{}, labelLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v11.attachments.golden.jsonl"), fakeSealer{}, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v11.attachments.plain.golden.jsonl"), nil, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v11.watches.golden.jsonl"), fakeSealer{}, watchLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v11.inline.golden.jsonl"), fakeSealer{}, inlineCommentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v11.revocations.golden.jsonl"), nil, revocationLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v12.golden.jsonl"), nil, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v12.sealed.golden.jsonl"), fakeSealer{}, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v12.richtext.golden.jsonl"), fakeSealer{}, richTextLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v12.comments.golden.jsonl"), fakeSealer{}, commentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v12.tree.golden.jsonl"), fakeSealer{}, treeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v12.blocks.golden.jsonl"), fakeSealer{}, blockKindsLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v12.labels.golden.jsonl"), fakeSealer{}, labelLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v12.attachments.golden.jsonl"), fakeSealer{}, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v12.attachments.plain.golden.jsonl"), nil, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v12.watches.golden.jsonl"), fakeSealer{}, watchLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v12.inline.golden.jsonl"), fakeSealer{}, inlineCommentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v12.revocations.golden.jsonl"), nil, revocationLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v12.provenance.golden.jsonl"), fakeSealer{}, provenanceLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v13.golden.jsonl"), nil, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v13.sealed.golden.jsonl"), fakeSealer{}, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v13.richtext.golden.jsonl"), fakeSealer{}, richTextLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v13.comments.golden.jsonl"), fakeSealer{}, commentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v13.tree.golden.jsonl"), fakeSealer{}, treeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v13.blocks.golden.jsonl"), fakeSealer{}, blockKindsLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v13.labels.golden.jsonl"), fakeSealer{}, labelLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v13.attachments.golden.jsonl"), fakeSealer{}, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v13.attachments.plain.golden.jsonl"), nil, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v13.watches.golden.jsonl"), fakeSealer{}, watchLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v13.inline.golden.jsonl"), fakeSealer{}, inlineCommentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v13.revocations.golden.jsonl"), nil, revocationLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v13.provenance.golden.jsonl"), fakeSealer{}, provenanceLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v13.imagesize.golden.jsonl"), fakeSealer{}, imageSizeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v14.golden.jsonl"), nil, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v14.sealed.golden.jsonl"), fakeSealer{}, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v14.richtext.golden.jsonl"), fakeSealer{}, richTextLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v14.comments.golden.jsonl"), fakeSealer{}, commentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v14.tree.golden.jsonl"), fakeSealer{}, treeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v14.blocks.golden.jsonl"), fakeSealer{}, blockKindsLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v14.labels.golden.jsonl"), fakeSealer{}, labelLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v14.attachments.golden.jsonl"), fakeSealer{}, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v14.attachments.plain.golden.jsonl"), nil, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v14.watches.golden.jsonl"), fakeSealer{}, watchLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v14.inline.golden.jsonl"), fakeSealer{}, inlineCommentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v14.revocations.golden.jsonl"), nil, revocationLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v14.provenance.golden.jsonl"), fakeSealer{}, provenanceLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v14.imagesize.golden.jsonl"), fakeSealer{}, imageSizeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v14.dataprotection.golden.jsonl"), fakeSealer{}, holdLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v14.dataprotection.plain.golden.jsonl"), nil, holdLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v15.golden.jsonl"), nil, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v15.sealed.golden.jsonl"), fakeSealer{}, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v15.richtext.golden.jsonl"), fakeSealer{}, richTextLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v15.comments.golden.jsonl"), fakeSealer{}, commentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v15.tree.golden.jsonl"), fakeSealer{}, treeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v15.blocks.golden.jsonl"), fakeSealer{}, blockKindsLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v15.labels.golden.jsonl"), fakeSealer{}, labelLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v15.attachments.golden.jsonl"), fakeSealer{}, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v15.attachments.plain.golden.jsonl"), nil, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v15.watches.golden.jsonl"), fakeSealer{}, watchLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v15.inline.golden.jsonl"), fakeSealer{}, inlineCommentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v15.revocations.golden.jsonl"), nil, revocationLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v15.provenance.golden.jsonl"), fakeSealer{}, provenanceLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v15.imagesize.golden.jsonl"), fakeSealer{}, imageSizeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v15.dataprotection.golden.jsonl"), fakeSealer{}, holdLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v15.dataprotection.plain.golden.jsonl"), nil, holdLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v15.layout.golden.jsonl"), fakeSealer{}, layoutLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v16.golden.jsonl"), nil, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v16.sealed.golden.jsonl"), fakeSealer{}, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v16.richtext.golden.jsonl"), fakeSealer{}, richTextLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v16.comments.golden.jsonl"), fakeSealer{}, commentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v16.tree.golden.jsonl"), fakeSealer{}, treeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v16.blocks.golden.jsonl"), fakeSealer{}, blockKindsLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v16.labels.golden.jsonl"), fakeSealer{}, labelLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v16.attachments.golden.jsonl"), fakeSealer{}, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v16.attachments.plain.golden.jsonl"), nil, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v16.watches.golden.jsonl"), fakeSealer{}, watchLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v16.inline.golden.jsonl"), fakeSealer{}, inlineCommentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v16.revocations.golden.jsonl"), nil, revocationLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v16.provenance.golden.jsonl"), fakeSealer{}, provenanceLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v16.imagesize.golden.jsonl"), fakeSealer{}, imageSizeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v16.dataprotection.golden.jsonl"), fakeSealer{}, holdLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v16.dataprotection.plain.golden.jsonl"), nil, holdLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v16.layout.golden.jsonl"), fakeSealer{}, layoutLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v16.kanbans.golden.jsonl"), nil, kanbanLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v17.golden.jsonl"), nil, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v17.sealed.golden.jsonl"), fakeSealer{}, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v17.richtext.golden.jsonl"), fakeSealer{}, richTextLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v17.comments.golden.jsonl"), fakeSealer{}, commentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v17.tree.golden.jsonl"), fakeSealer{}, treeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v17.blocks.golden.jsonl"), fakeSealer{}, blockKindsLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v17.labels.golden.jsonl"), fakeSealer{}, labelLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v17.attachments.golden.jsonl"), fakeSealer{}, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v17.attachments.plain.golden.jsonl"), nil, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v17.watches.golden.jsonl"), fakeSealer{}, watchLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v17.inline.golden.jsonl"), fakeSealer{}, inlineCommentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v17.revocations.golden.jsonl"), nil, revocationLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v17.provenance.golden.jsonl"), fakeSealer{}, provenanceLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v17.imagesize.golden.jsonl"), fakeSealer{}, imageSizeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v17.dataprotection.golden.jsonl"), fakeSealer{}, holdLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v17.dataprotection.plain.golden.jsonl"), nil, holdLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v17.layout.golden.jsonl"), fakeSealer{}, layoutLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v17.kanbans.golden.jsonl"), nil, kanbanLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v17.spaces.golden.jsonl"), nil, spaceLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v18.golden.jsonl"), nil, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v18.sealed.golden.jsonl"), fakeSealer{}, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v18.richtext.golden.jsonl"), fakeSealer{}, richTextLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v18.comments.golden.jsonl"), fakeSealer{}, commentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v18.tree.golden.jsonl"), fakeSealer{}, treeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v18.blocks.golden.jsonl"), fakeSealer{}, blockKindsLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v18.labels.golden.jsonl"), fakeSealer{}, labelLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v18.attachments.golden.jsonl"), fakeSealer{}, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v18.attachments.plain.golden.jsonl"), nil, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v18.watches.golden.jsonl"), fakeSealer{}, watchLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v18.inline.golden.jsonl"), fakeSealer{}, inlineCommentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v18.revocations.golden.jsonl"), nil, revocationLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v18.provenance.golden.jsonl"), fakeSealer{}, provenanceLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v18.imagesize.golden.jsonl"), fakeSealer{}, imageSizeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v18.dataprotection.golden.jsonl"), fakeSealer{}, holdLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v18.dataprotection.plain.golden.jsonl"), nil, holdLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v18.layout.golden.jsonl"), fakeSealer{}, layoutLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v18.kanbans.golden.jsonl"), nil, kanbanLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v18.spaces.golden.jsonl"), nil, spaceLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v18.catalog.golden.jsonl"), nil, catalogLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v19.golden.jsonl"), nil, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v19.sealed.golden.jsonl"), fakeSealer{}, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v19.richtext.golden.jsonl"), fakeSealer{}, richTextLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v19.comments.golden.jsonl"), fakeSealer{}, commentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v19.tree.golden.jsonl"), fakeSealer{}, treeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v19.blocks.golden.jsonl"), fakeSealer{}, blockKindsLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v19.labels.golden.jsonl"), fakeSealer{}, labelLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v19.attachments.golden.jsonl"), fakeSealer{}, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v19.attachments.plain.golden.jsonl"), nil, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v19.watches.golden.jsonl"), fakeSealer{}, watchLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v19.inline.golden.jsonl"), fakeSealer{}, inlineCommentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v19.revocations.golden.jsonl"), nil, revocationLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v19.provenance.golden.jsonl"), fakeSealer{}, provenanceLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v19.imagesize.golden.jsonl"), fakeSealer{}, imageSizeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v19.dataprotection.golden.jsonl"), fakeSealer{}, holdLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v19.dataprotection.plain.golden.jsonl"), nil, holdLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v19.layout.golden.jsonl"), fakeSealer{}, layoutLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v19.kanbans.golden.jsonl"), nil, kanbanLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v19.spaces.golden.jsonl"), nil, spaceLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v19.catalog.golden.jsonl"), nil, catalogLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v19.kanbancolumns.golden.jsonl"), nil, kanbanColumnsLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v20.golden.jsonl"), nil, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v20.sealed.golden.jsonl"), fakeSealer{}, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v20.richtext.golden.jsonl"), fakeSealer{}, richTextLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v20.comments.golden.jsonl"), fakeSealer{}, commentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v20.tree.golden.jsonl"), fakeSealer{}, treeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v20.blocks.golden.jsonl"), fakeSealer{}, blockKindsLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v20.labels.golden.jsonl"), fakeSealer{}, labelLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v20.attachments.golden.jsonl"), fakeSealer{}, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v20.attachments.plain.golden.jsonl"), nil, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v20.watches.golden.jsonl"), fakeSealer{}, watchLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v20.inline.golden.jsonl"), fakeSealer{}, inlineCommentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v20.revocations.golden.jsonl"), nil, revocationLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v20.provenance.golden.jsonl"), fakeSealer{}, provenanceLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v20.imagesize.golden.jsonl"), fakeSealer{}, imageSizeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v20.dataprotection.golden.jsonl"), fakeSealer{}, holdLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v20.dataprotection.plain.golden.jsonl"), nil, holdLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v20.layout.golden.jsonl"), fakeSealer{}, layoutLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v20.kanbans.golden.jsonl"), nil, kanbanLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v20.spaces.golden.jsonl"), nil, spaceLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v20.catalog.golden.jsonl"), nil, catalogLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v20.kanbancolumns.golden.jsonl"), nil, kanbanColumnsLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v20.spacenames.golden.jsonl"), nil, spaceNameLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v21.golden.jsonl"), nil, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v21.sealed.golden.jsonl"), fakeSealer{}, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v21.richtext.golden.jsonl"), fakeSealer{}, richTextLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v21.comments.golden.jsonl"), fakeSealer{}, commentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v21.tree.golden.jsonl"), fakeSealer{}, treeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v21.blocks.golden.jsonl"), fakeSealer{}, blockKindsLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v21.labels.golden.jsonl"), fakeSealer{}, labelLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v21.attachments.golden.jsonl"), fakeSealer{}, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v21.attachments.plain.golden.jsonl"), nil, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v21.watches.golden.jsonl"), fakeSealer{}, watchLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v21.inline.golden.jsonl"), fakeSealer{}, inlineCommentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v21.revocations.golden.jsonl"), nil, revocationLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v21.provenance.golden.jsonl"), fakeSealer{}, provenanceLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v21.imagesize.golden.jsonl"), fakeSealer{}, imageSizeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v21.dataprotection.golden.jsonl"), fakeSealer{}, holdLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v21.dataprotection.plain.golden.jsonl"), nil, holdLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v21.layout.golden.jsonl"), fakeSealer{}, layoutLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v21.kanbans.golden.jsonl"), nil, kanbanLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v21.spaces.golden.jsonl"), nil, spaceLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v21.catalog.golden.jsonl"), nil, catalogLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v21.kanbancolumns.golden.jsonl"), nil, kanbanColumnsLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v21.spacenames.golden.jsonl"), nil, spaceNameLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v21.secrets.golden.jsonl"), fakeSealer{}, secretLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v21.secrets.plain.golden.jsonl"), nil, secretLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v22.golden.jsonl"), nil, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v22.sealed.golden.jsonl"), fakeSealer{}, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v22.richtext.golden.jsonl"), fakeSealer{}, richTextLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v22.comments.golden.jsonl"), fakeSealer{}, commentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v22.tree.golden.jsonl"), fakeSealer{}, treeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v22.blocks.golden.jsonl"), fakeSealer{}, blockKindsLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v22.labels.golden.jsonl"), fakeSealer{}, labelLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v22.attachments.golden.jsonl"), fakeSealer{}, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v22.attachments.plain.golden.jsonl"), nil, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v22.watches.golden.jsonl"), fakeSealer{}, watchLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v22.inline.golden.jsonl"), fakeSealer{}, inlineCommentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v22.revocations.golden.jsonl"), nil, revocationLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v22.provenance.golden.jsonl"), fakeSealer{}, provenanceLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v22.imagesize.golden.jsonl"), fakeSealer{}, imageSizeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v22.dataprotection.golden.jsonl"), fakeSealer{}, holdLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v22.dataprotection.plain.golden.jsonl"), nil, holdLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v22.layout.golden.jsonl"), fakeSealer{}, layoutLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v22.kanbans.golden.jsonl"), nil, kanbanLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v22.spaces.golden.jsonl"), nil, spaceLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v22.catalog.golden.jsonl"), nil, catalogLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v22.kanbancolumns.golden.jsonl"), nil, kanbanColumnsLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v22.spacenames.golden.jsonl"), nil, spaceNameLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v22.secrets.golden.jsonl"), fakeSealer{}, secretLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v22.secrets.plain.golden.jsonl"), nil, secretLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v22.secreterasure.golden.jsonl"), fakeSealer{}, secretErasureLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v22.secreterasure.plain.golden.jsonl"), nil, secretErasureLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v23.golden.jsonl"), nil, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v23.sealed.golden.jsonl"), fakeSealer{}, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v23.richtext.golden.jsonl"), fakeSealer{}, richTextLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v23.comments.golden.jsonl"), fakeSealer{}, commentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v23.tree.golden.jsonl"), fakeSealer{}, treeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v23.blocks.golden.jsonl"), fakeSealer{}, blockKindsLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v23.labels.golden.jsonl"), fakeSealer{}, labelLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v23.attachments.golden.jsonl"), fakeSealer{}, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v23.attachments.plain.golden.jsonl"), nil, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v23.watches.golden.jsonl"), fakeSealer{}, watchLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v23.inline.golden.jsonl"), fakeSealer{}, inlineCommentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v23.revocations.golden.jsonl"), nil, revocationLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v23.provenance.golden.jsonl"), fakeSealer{}, provenanceLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v23.imagesize.golden.jsonl"), fakeSealer{}, imageSizeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v23.dataprotection.golden.jsonl"), fakeSealer{}, holdLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v23.dataprotection.plain.golden.jsonl"), nil, holdLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v23.layout.golden.jsonl"), fakeSealer{}, layoutLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v23.kanbans.golden.jsonl"), nil, kanbanLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v23.spaces.golden.jsonl"), nil, spaceLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v23.catalog.golden.jsonl"), nil, catalogLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v23.kanbancolumns.golden.jsonl"), nil, kanbanColumnsLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v23.spacenames.golden.jsonl"), nil, spaceNameLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v23.secrets.golden.jsonl"), fakeSealer{}, secretLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v23.secrets.plain.golden.jsonl"), nil, secretLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v23.secreterasure.golden.jsonl"), fakeSealer{}, secretErasureLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v23.secreterasure.plain.golden.jsonl"), nil, secretErasureLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v23.album.golden.jsonl"), fakeSealer{}, albumLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v23.album.plain.golden.jsonl"), nil, albumLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v24.golden.jsonl"), nil, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v24.sealed.golden.jsonl"), fakeSealer{}, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v24.richtext.golden.jsonl"), fakeSealer{}, richTextLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v24.comments.golden.jsonl"), fakeSealer{}, commentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v24.tree.golden.jsonl"), fakeSealer{}, treeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v24.blocks.golden.jsonl"), fakeSealer{}, blockKindsLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v24.labels.golden.jsonl"), fakeSealer{}, labelLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v24.attachments.golden.jsonl"), fakeSealer{}, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v24.attachments.plain.golden.jsonl"), nil, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v24.watches.golden.jsonl"), fakeSealer{}, watchLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v24.inline.golden.jsonl"), fakeSealer{}, inlineCommentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v24.revocations.golden.jsonl"), nil, revocationLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v24.provenance.golden.jsonl"), fakeSealer{}, provenanceLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v24.imagesize.golden.jsonl"), fakeSealer{}, imageSizeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v24.dataprotection.golden.jsonl"), fakeSealer{}, holdLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v24.dataprotection.plain.golden.jsonl"), nil, holdLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v24.layout.golden.jsonl"), fakeSealer{}, layoutLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v24.kanbans.golden.jsonl"), nil, kanbanLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v24.spaces.golden.jsonl"), nil, spaceLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v24.catalog.golden.jsonl"), nil, catalogLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v24.kanbancolumns.golden.jsonl"), nil, kanbanColumnsLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v24.spacenames.golden.jsonl"), nil, spaceNameLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v24.secrets.golden.jsonl"), fakeSealer{}, secretLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v24.secrets.plain.golden.jsonl"), nil, secretLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v24.secreterasure.golden.jsonl"), fakeSealer{}, secretErasureLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v24.secreterasure.plain.golden.jsonl"), nil, secretErasureLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v24.album.golden.jsonl"), fakeSealer{}, albumLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v24.album.plain.golden.jsonl"), nil, albumLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v24.typeremoval.golden.jsonl"), nil, typeRemovalLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v25.golden.jsonl"), nil, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v25.sealed.golden.jsonl"), fakeSealer{}, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v25.richtext.golden.jsonl"), fakeSealer{}, richTextLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v25.comments.golden.jsonl"), fakeSealer{}, commentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v25.tree.golden.jsonl"), fakeSealer{}, treeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v25.blocks.golden.jsonl"), fakeSealer{}, blockKindsLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v25.labels.golden.jsonl"), fakeSealer{}, labelLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v25.attachments.golden.jsonl"), fakeSealer{}, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v25.attachments.plain.golden.jsonl"), nil, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v25.watches.golden.jsonl"), fakeSealer{}, watchLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v25.inline.golden.jsonl"), fakeSealer{}, inlineCommentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v25.revocations.golden.jsonl"), nil, revocationLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v25.provenance.golden.jsonl"), fakeSealer{}, provenanceLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v25.imagesize.golden.jsonl"), fakeSealer{}, imageSizeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v25.dataprotection.golden.jsonl"), fakeSealer{}, holdLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v25.dataprotection.plain.golden.jsonl"), nil, holdLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v25.layout.golden.jsonl"), fakeSealer{}, layoutLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v25.kanbans.golden.jsonl"), nil, kanbanLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v25.spaces.golden.jsonl"), nil, spaceLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v25.catalog.golden.jsonl"), nil, catalogLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v25.kanbancolumns.golden.jsonl"), nil, kanbanColumnsLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v25.spacenames.golden.jsonl"), nil, spaceNameLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v25.secrets.golden.jsonl"), fakeSealer{}, secretLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v25.secrets.plain.golden.jsonl"), nil, secretLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v25.secreterasure.golden.jsonl"), fakeSealer{}, secretErasureLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v25.secreterasure.plain.golden.jsonl"), nil, secretErasureLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v25.album.golden.jsonl"), fakeSealer{}, albumLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v25.album.plain.golden.jsonl"), nil, albumLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v25.typeremoval.golden.jsonl"), nil, typeRemovalLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v25.video.golden.jsonl"), fakeSealer{}, videoLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v25.video.plain.golden.jsonl"), nil, videoLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v26.golden.jsonl"), nil, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v26.sealed.golden.jsonl"), fakeSealer{}, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v26.richtext.golden.jsonl"), fakeSealer{}, richTextLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v26.comments.golden.jsonl"), fakeSealer{}, commentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v26.tree.golden.jsonl"), fakeSealer{}, treeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v26.blocks.golden.jsonl"), fakeSealer{}, blockKindsLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v26.labels.golden.jsonl"), fakeSealer{}, labelLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v26.attachments.golden.jsonl"), fakeSealer{}, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v26.attachments.plain.golden.jsonl"), nil, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v26.watches.golden.jsonl"), fakeSealer{}, watchLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v26.inline.golden.jsonl"), fakeSealer{}, inlineCommentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v26.revocations.golden.jsonl"), nil, revocationLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v26.provenance.golden.jsonl"), fakeSealer{}, provenanceLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v26.imagesize.golden.jsonl"), fakeSealer{}, imageSizeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v26.dataprotection.golden.jsonl"), fakeSealer{}, holdLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v26.dataprotection.plain.golden.jsonl"), nil, holdLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v26.layout.golden.jsonl"), fakeSealer{}, layoutLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v26.kanbans.golden.jsonl"), nil, kanbanLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v26.spaces.golden.jsonl"), nil, spaceLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v26.catalog.golden.jsonl"), nil, catalogLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v26.kanbancolumns.golden.jsonl"), nil, kanbanColumnsLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v26.spacenames.golden.jsonl"), nil, spaceNameLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v26.secrets.golden.jsonl"), fakeSealer{}, secretLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v26.secrets.plain.golden.jsonl"), nil, secretLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v26.secreterasure.golden.jsonl"), fakeSealer{}, secretErasureLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v26.secreterasure.plain.golden.jsonl"), nil, secretErasureLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v26.album.golden.jsonl"), fakeSealer{}, albumLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v26.album.plain.golden.jsonl"), nil, albumLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v26.typeremoval.golden.jsonl"), nil, typeRemovalLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v26.video.golden.jsonl"), fakeSealer{}, videoLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v26.video.plain.golden.jsonl"), nil, videoLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v26.retention.golden.jsonl"), fakeSealer{}, retentionLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v26.retention.plain.golden.jsonl"), nil, retentionLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v27.golden.jsonl"), nil, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v27.sealed.golden.jsonl"), fakeSealer{}, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v27.richtext.golden.jsonl"), fakeSealer{}, richTextLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v27.comments.golden.jsonl"), fakeSealer{}, commentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v27.tree.golden.jsonl"), fakeSealer{}, treeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v27.blocks.golden.jsonl"), fakeSealer{}, blockKindsLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v27.labels.golden.jsonl"), fakeSealer{}, labelLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v27.attachments.golden.jsonl"), fakeSealer{}, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v27.attachments.plain.golden.jsonl"), nil, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v27.watches.golden.jsonl"), fakeSealer{}, watchLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v27.inline.golden.jsonl"), fakeSealer{}, inlineCommentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v27.revocations.golden.jsonl"), nil, revocationLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v27.provenance.golden.jsonl"), fakeSealer{}, provenanceLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v27.imagesize.golden.jsonl"), fakeSealer{}, imageSizeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v27.dataprotection.golden.jsonl"), fakeSealer{}, holdLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v27.dataprotection.plain.golden.jsonl"), nil, holdLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v27.layout.golden.jsonl"), fakeSealer{}, layoutLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v27.kanbans.golden.jsonl"), nil, kanbanLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v27.spaces.golden.jsonl"), nil, spaceLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v27.catalog.golden.jsonl"), nil, catalogLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v27.kanbancolumns.golden.jsonl"), nil, kanbanColumnsLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v27.spacenames.golden.jsonl"), nil, spaceNameLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v27.secrets.golden.jsonl"), fakeSealer{}, secretLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v27.secrets.plain.golden.jsonl"), nil, secretLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v27.secreterasure.golden.jsonl"), fakeSealer{}, secretErasureLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v27.secreterasure.plain.golden.jsonl"), nil, secretErasureLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v27.album.golden.jsonl"), fakeSealer{}, albumLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v27.album.plain.golden.jsonl"), nil, albumLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v27.typeremoval.golden.jsonl"), nil, typeRemovalLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v27.video.golden.jsonl"), fakeSealer{}, videoLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v27.video.plain.golden.jsonl"), nil, videoLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v27.retention.golden.jsonl"), fakeSealer{}, retentionLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v27.retention.plain.golden.jsonl"), nil, retentionLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v27.tasklist.golden.jsonl"), fakeSealer{}, taskListLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v27.tasklist.plain.golden.jsonl"), nil, taskListLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.golden.jsonl"), nil, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v28.sealed.golden.jsonl"), fakeSealer{}, log)
	assertRebuilds(t, filepath.Join("testdata", "events.v28.richtext.golden.jsonl"), fakeSealer{}, richTextLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.comments.golden.jsonl"), fakeSealer{}, commentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.tree.golden.jsonl"), fakeSealer{}, treeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.blocks.golden.jsonl"), fakeSealer{}, blockKindsLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.labels.golden.jsonl"), fakeSealer{}, labelLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.attachments.golden.jsonl"), fakeSealer{}, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.attachments.plain.golden.jsonl"), nil, attachmentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.watches.golden.jsonl"), fakeSealer{}, watchLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.inline.golden.jsonl"), fakeSealer{}, inlineCommentLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.revocations.golden.jsonl"), nil, revocationLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.provenance.golden.jsonl"), fakeSealer{}, provenanceLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.imagesize.golden.jsonl"), fakeSealer{}, imageSizeLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.dataprotection.golden.jsonl"), fakeSealer{}, holdLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.dataprotection.plain.golden.jsonl"), nil, holdLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.layout.golden.jsonl"), fakeSealer{}, layoutLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.kanbans.golden.jsonl"), nil, kanbanLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.spaces.golden.jsonl"), nil, spaceLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.catalog.golden.jsonl"), nil, catalogLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.kanbancolumns.golden.jsonl"), nil, kanbanColumnsLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.spacenames.golden.jsonl"), nil, spaceNameLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.secrets.golden.jsonl"), fakeSealer{}, secretLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.secrets.plain.golden.jsonl"), nil, secretLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.secreterasure.golden.jsonl"), fakeSealer{}, secretErasureLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.secreterasure.plain.golden.jsonl"), nil, secretErasureLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.album.golden.jsonl"), fakeSealer{}, albumLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.album.plain.golden.jsonl"), nil, albumLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.typeremoval.golden.jsonl"), nil, typeRemovalLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.video.golden.jsonl"), fakeSealer{}, videoLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.video.plain.golden.jsonl"), nil, videoLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.retention.golden.jsonl"), fakeSealer{}, retentionLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.retention.plain.golden.jsonl"), nil, retentionLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.tasklist.golden.jsonl"), fakeSealer{}, taskListLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.tasklist.plain.golden.jsonl"), nil, taskListLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.lists.golden.jsonl"), fakeSealer{}, listsLog(t))
	assertRebuilds(t, filepath.Join("testdata", "events.v28.lists.plain.golden.jsonl"), nil, listsLog(t))
}

// provenanceLog copies a file to another item, makes a new version of the
// original, and deletes the version copied, so the golden file pins sealed
// sources and the source kept by a deleted file (ADR-0055).
func provenanceLog(t *testing.T) []Event {
	s := setup(t)
	page := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Plans"}))
	other := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))
	file := func(id string, src *Source) Attachment {
		return Attachment{ID: ID(id), Name: "plan.drawing.png", Type: "image/png", Size: 3, SHA256: strings.Repeat("ab", 32), Source: src}
	}
	const v1, v2, c1 = "0123456789abcdef0123456789abcdef", "fedcba9876543210fedcba9876543210", "00000000000000000000000000000001"
	for _, f := range []struct {
		item ID
		a    Attachment
	}{
		{page.ID, file(v1, nil)},
		{other.ID, file(c1, &Source{Item: page.ID, Attachment: v1})},
		{page.ID, file(v2, &Source{Item: page.ID, Attachment: v1})},
	} {
		if _, err := s.AddAttachment("anna", f.item, f.a); err != nil {
			t.Fatal(err)
		}
	}
	noErr(t, s.DeleteAttachment("anna", page.ID, v1))
	return s.Log()
}

// testAssessor finds the word "Diagnose" as an indicator of health data,
// for the logs and tests of data protection review (ADR-0058).
func testAssessor(texts []string) []DPFinding {
	for _, t := range texts {
		if strings.Contains(t, "Diagnose") {
			return []DPFinding{{Regulation: "dsg", Category: "health", Indicator: "Diagnose"}}
		}
	}
	return nil
}

// holdLog holds a page for health data, releases it as a false alarm,
// holds another and releases it once restricted, then purges the first, so
// the golden files pin sealed holds and releases and their erasure.
func holdLog(t *testing.T) []Event {
	s := setup(t)
	s.SetAssessor(testAssessor)
	noErr(t, s.GrantRole("admin", "PLAT", "pat", SpaceAdmin))
	a := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Visit"}))
	noErr(t, s.CommitBody("anna", a.ID, Doc{Blocks: []Block{{Kind: Paragraph, Text: "Diagnose: flu"}}}))
	noErr(t, s.As("pat").ReleaseHold(a.ID, HoldFalseAlarm, "a software diagnosis"))
	b := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Diagnose report"}))
	noErr(t, s.Restrict("anna", b.ID, Restriction{Read: []string{"anna"}}))
	noErr(t, s.As("pat").ReleaseHold(b.ID, HoldRestricted, ""))
	noErr(t, s.Trash("anna", a.ID))
	noErr(t, s.Purge("anna", a.ID))
	return s.Log()
}

// kanbanLog makes two kanbans, renames one and removes the other, so the
// golden file pins kanban definitions (ADR-0060).
// catalogLog defines a type with a url field, a unique and a recommended
// field and a review rule, and fills it, so the golden file pins them
// (ADR-0074).
func catalogLog(t *testing.T) []Event {
	s := setup(t)
	noErr(t, s.DefineType("admin", TypeDef{Name: "interface", Fields: []FieldDef{
		{Name: "source-id", Kind: KindText, Unique: true},
		{Name: "specification", Kind: KindURL},
		{Name: "contact", Kind: KindText, Recommended: true},
		{Name: "owner", Kind: KindUser},
		{Name: "reviewed", Kind: KindDate},
	}, Review: &Review{EveryDays: 365, Field: "reviewed", Owner: "owner"}}))
	it := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "interface", Title: "Weather",
		Properties: Props{"source-id": "meteo-1", "specification": "https://example.org/openapi.json"}}))
	noErr(t, s.SetProperty("anna", it.ID, "owner", "anna"))
	noErr(t, s.GrantRole("admin", "PLAT", "anna", Editor))
	noErr(t, s.As("anna").MarkReviewed(it.ID, time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)))
	return s.Log()
}

// spaceLog puts a space into the trash, brings it back, and deletes it for
// good, so the golden file pins the space kinds (ADR-0069).
func spaceLog(t *testing.T) []Event {
	s := setup(t)
	mustItem(t)(s.Create("anna", NewItem{Space: "OPS", Type: "page", Title: "Old"}))
	noErr(t, s.TrashSpace("admin", "OPS"))
	noErr(t, s.RestoreSpace("admin", "OPS"))
	noErr(t, s.TrashSpace("admin", "OPS"))
	noErr(t, s.PurgeSpace("admin", "OPS"))
	return s.Log()
}

func kanbanLog(t *testing.T) []Event {
	s := setup(t)
	noErr(t, s.GrantRole("admin", "PLAT", "anna", Editor))
	anna := s.As("anna")
	web, err := anna.SetKanban("PLAT", Kanban{Name: "Website relaunch", Type: "task"})
	noErr(t, err)
	ops, err := anna.SetKanban("PLAT", Kanban{Name: "Operations", Type: "task", Label: "ops"})
	noErr(t, err)
	web.Name = "Website"
	_, err = anna.SetKanban("PLAT", web)
	noErr(t, err)
	noErr(t, anna.RemoveKanban("PLAT", ops.ID))
	return s.Log()
}

// kanbanColumnsLog gives a kanban columns of its own and fields on its
// cards, and puts a card in a column, so the golden file pins them
// (ADR-0076).
func kanbanColumnsLog(t *testing.T) []Event {
	s := setup(t)
	noErr(t, s.GrantRole("admin", "PLAT", "anna", Editor))
	anna := s.As("anna")
	web, err := anna.SetKanban("PLAT", Kanban{Name: "Website", Type: "task"})
	noErr(t, err)
	web.Columns = []KanbanColumn{{Name: "To do", State: "open"}, {Name: "Review"}, {Name: "Doing", State: "in progress"}, {Name: "Done", State: "done"}}
	web.CardFields = []string{"assignee", "labels"}
	web, err = anna.SetKanban("PLAT", web)
	noErr(t, err)
	card := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "task", Title: "Card", Properties: Props{"priority": "low"}}))
	noErr(t, s.SetLabels("anna", card.ID, []string{web.Label}))
	noErr(t, anna.PlaceCard("PLAT", web.ID, card.ID, web.Columns[1].ID, ""))
	return s.Log()
}

// secretLog protects a passage, asks for it, grants and declines, sets
// its readers and takes the protection off, so the golden files pin the
// passage kinds and the sealing of a passage apart (ADR-0088).
func secretLog(t *testing.T) []Event {
	s := setup(t)
	noErr(t, s.GrantRole("admin", "PLAT", "anna", Editor))
	noErr(t, s.GrantRole("admin", "PLAT", "bert", Viewer))
	noErr(t, s.GrantRole("admin", "PLAT", "carl", Viewer))
	anna := s.As("anna")
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Review",
		Body: &Doc{Blocks: []Block{{Kind: Paragraph, Text: "Salary of Paul is due."}}}}))
	sec, err := anna.Protect(page.ID, 0, 10, 14, []string{"@hr"})
	noErr(t, err)
	noErr(t, s.As("bert").RequestSecret(page.ID, sec.ID))
	noErr(t, anna.DecideSecret(page.ID, sec.ID, "bert", true))
	noErr(t, s.As("carl").RequestSecret(page.ID, sec.ID))
	noErr(t, anna.DecideSecret(page.ID, sec.ID, "carl", false))
	noErr(t, anna.SetSecretReaders(page.ID, sec.ID, []string{"@hr", "bert", "carl"}))
	noErr(t, anna.Unprotect(page.ID, sec.ID))
	return s.Log()
}

// albumLog makes a folder of the album with a picture and labels it, so
// the golden file pins the folder type and the kind AttachmentLabelled
// (ADR-0090).
func albumLog(t *testing.T) []Event {
	s := setup(t)
	noErr(t, s.GrantRole("admin", "PLAT", "anna", Editor))
	anna := s.As("anna")
	folder := mustItem(t)(anna.CreateFolder("PLAT", "", "Logos"))
	const logo = "0123456789abcdef0123456789abcdef"
	_, err := anna.AddAttachment(folder.ID, Attachment{ID: logo, Name: "logo.png", Type: "image/png", Size: 3, SHA256: strings.Repeat("ab", 32)})
	noErr(t, err)
	noErr(t, anna.SetPictureLabels(folder.ID, logo, []string{"Brand", "print"}))
	return s.Log()
}

// secretErasureLog erases a protected passage that an inline comment
// quotes, so the golden file pins the kind SecretErased (ADR-0089).
func secretErasureLog(t *testing.T) []Event {
	s := setup(t)
	noErr(t, s.GrantRole("admin", "PLAT", "anna", Editor))
	anna := s.As("anna")
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Review",
		Body: &Doc{Blocks: []Block{{Kind: Paragraph, Text: "Salary of Paul is due."}}}}))
	sec, err := anna.Protect(page.ID, 0, 10, 14, nil)
	noErr(t, err)
	_, err = anna.Comment(page.ID, NewComment{Text: "Check", Anchor: &Anchor{Quote: "Paul", Prefix: "Salary of "}})
	noErr(t, err)
	noErr(t, anna.EraseSecret(page.ID, sec.ID))
	return s.Log()
}

// videoLog plays a video in a text, so the golden files pin the block
// kind video (ADR-0097).
func videoLog(t *testing.T) []Event {
	s := setup(t)
	noErr(t, s.GrantRole("admin", "PLAT", "anna", Editor))
	anna := s.As("anna")
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Demo"}))
	const clip = "fedcba9876543210fedcba9876543210"
	_, err := anna.AddAttachment(page.ID, Attachment{ID: clip, Name: "demo.mp4", Type: "video/mp4", Size: 3, SHA256: strings.Repeat("cd", 32)})
	noErr(t, err)
	noErr(t, anna.CommitBody(page.ID, Doc{Blocks: []Block{{Kind: Video, Ref: clip, Text: "The demo", Width: 60, Align: "center"}}}))
	return s.Log()
}

// retentionLog gives the versions of a drawing a rule of their own,
// cleans them up and returns them to the default, so the golden files pin
// the kind AttachmentRetentionSet (ADR-0104).
func retentionLog(t *testing.T) []Event {
	s := setup(t)
	noErr(t, s.GrantRole("admin", "PLAT", "anna", Editor))
	anna := s.As("anna")
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Plan"}))
	const v1, v2 = "0123456789abcdef0123456789abcde1", "0123456789abcdef0123456789abcde2"
	_, err := anna.AddAttachment(page.ID, Attachment{ID: v1, Name: "plan.drawing.png", Type: "image/png", Size: 3, SHA256: strings.Repeat("ab", 32)})
	noErr(t, err)
	_, err = anna.AddAttachment(page.ID, Attachment{ID: v2, Name: "plan.drawing.png", Type: "image/png", Size: 3, SHA256: strings.Repeat("cd", 32), Source: &Source{Item: page.ID, Attachment: v1}})
	noErr(t, err)
	noErr(t, anna.SetPictureRetention(page.ID, v2, &Retention{Keep: 1, Days: 30}))
	_, err = anna.CleanVersions(page.ID, v2, nil, Retention{}, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
	noErr(t, err)
	noErr(t, anna.SetPictureRetention(page.ID, v2, nil))
	return s.Log()
}

// listsLog puts a list of figures and a list of tables in a text, so the
// golden files pin the block kinds figures and tables (ADR-0119).
func listsLog(t *testing.T) []Event {
	s := setup(t)
	noErr(t, s.GrantRole("admin", "PLAT", "anna", Editor))
	anna := s.As("anna")
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Report"}))
	noErr(t, anna.CommitBody(page.ID, Doc{Blocks: []Block{{Kind: Figures}, {Kind: Tables}, {Kind: Table, Text: "Item\tPrice\nTea\t3"}}}))
	return s.Log()
}

// bpmnLog keeps a BPMN model as a file of a page and shows it, and shows a
// process of an engine beside it, so the golden files pin the block kinds
// bpmn and process and the fields Engine and Process (ADR-0129).
func bpmnLog(t *testing.T) []Event {
	s := setup(t)
	noErr(t, s.GrantRole("admin", "PLAT", "anna", Editor))
	anna := s.As("anna")
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Order to cash"}))
	const model = "00112233445566778899aabbccddeeff"
	_, err := anna.AddAttachment(page.ID, Attachment{ID: model, Name: "order.bpmn", Type: "text/xml; charset=utf-8", Size: 3, SHA256: strings.Repeat("ef", 32)})
	noErr(t, err)
	noErr(t, anna.CommitBody(page.ID, Doc{Blocks: []Block{
		{Kind: BPMN, Ref: model, Text: "As drawn"},
		{Kind: Process, Engine: "atlas-prod", Process: "order-to-cash", Text: "As it runs"},
	}}))
	return s.Log()
}

// taskListLog lists the tasks of a label in a text, so the golden files
// pin the block kind tasklist (ADR-0114).
func taskListLog(t *testing.T) []Event {
	s := setup(t)
	noErr(t, s.GrantRole("admin", "PLAT", "anna", Editor))
	anna := s.As("anna")
	page := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "page", Title: "Release"}))
	noErr(t, anna.CommitBody(page.ID, Doc{Blocks: []Block{{Kind: Paragraph, Text: "Open work:"}, {Kind: TaskList, Text: "release-2"}}}))
	return s.Log()
}

// typeRemovalLog defines a type, makes and purges an item of it and
// removes the type, so the golden file pins the kind TypeRemoved
// (ADR-0094).
func typeRemovalLog(t *testing.T) []Event {
	s := setup(t)
	noErr(t, s.DefineType("admin", TypeDef{Name: "interface", Body: true}))
	noErr(t, s.GrantRole("admin", "PLAT", "anna", SpaceAdmin))
	anna := s.As("anna")
	it := mustItem(t)(anna.Create(NewItem{Space: "PLAT", Type: "interface", Title: "Old entry"}))
	noErr(t, anna.Trash(it.ID))
	noErr(t, anna.Purge(it.ID))
	noErr(t, s.RemoveType("admin", "interface"))
	return s.Log()
}

// spaceNameLog renames a space, so the golden file pins the kind
// SpaceRenamed (ADR-0086).
func spaceNameLog(t *testing.T) []Event {
	s := setup(t)
	noErr(t, s.RenameSpace("admin", "PLAT", "Platform team"))
	return s.Log()
}

// layoutLog writes aligned text and images, a divider and the marks
// underline, strike and highlight, so the golden file pins them (ADR-0059).
func layoutLog(t *testing.T) []Event {
	s := setup(t)
	page := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Layout"}))
	const plan = "0123456789abcdef0123456789abcdef"
	if _, err := s.AddAttachment("anna", page.ID, Attachment{ID: plan, Name: "plan.png", Type: "image/png", Size: 3, SHA256: strings.Repeat("ab", 32)}); err != nil {
		t.Fatal(err)
	}
	noErr(t, s.CommitBody("anna", page.ID, Doc{Blocks: []Block{
		{Kind: Heading, Level: 1, Text: "Centred", Align: "center"},
		{Kind: Paragraph, Text: "under struck marked", Align: "justify", Marks: []Mark{
			{Type: MarkUnderline, Start: 0, End: 5}, {Type: MarkStrike, Start: 6, End: 12}, {Type: MarkHighlight, Start: 13, End: 19}}},
		{Kind: Divider},
		{Kind: Image, Ref: plan, Text: "Plan", Width: 40, Align: "right"},
	}}))
	return s.Log()
}

// imageSizeLog shows an image at a width and then at its own size, so the
// golden file pins the width of image blocks (ADR-0056).
func imageSizeLog(t *testing.T) []Event {
	s := setup(t)
	page := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Plans"}))
	const plan = "0123456789abcdef0123456789abcdef"
	if _, err := s.AddAttachment("anna", page.ID, Attachment{ID: plan, Name: "plan.png", Type: "image/png", Size: 3, SHA256: strings.Repeat("ab", 32)}); err != nil {
		t.Fatal(err)
	}
	noErr(t, s.CommitBody("anna", page.ID, Doc{Blocks: []Block{{Kind: Image, Ref: plan, Text: "The plan", Width: 50}}}))
	noErr(t, s.CommitBody("anna", page.ID, Doc{Blocks: []Block{{Kind: Image, Ref: plan, Text: "The plan"}}}))
	return s.Log()
}

// revocationLog grants and takes back a space role, a group membership and
// a system role (ADR-0048).
func revocationLog(t *testing.T) []Event {
	s := setup(t)
	noErr(t, s.GrantRole("admin", "PLAT", "anna", Editor))
	noErr(t, s.RevokeRole("admin", "PLAT", "anna"))
	noErr(t, s.AddToGroup("admin", "ops", "bert"))
	noErr(t, s.RemoveFromGroup("admin", "ops", "bert"))
	noErr(t, s.GrantSystemRole("admin", "root", RoleAdmin))
	noErr(t, s.GrantSystemRole("admin", "erin", RoleAdmin))
	noErr(t, s.RevokeSystemRole("root", "erin", RoleAdmin))
	return s.Log()
}

// inlineCommentLog comments on a passage, answers it, resolves and reopens
// the thread, and purges an item with an inline comment, so the golden file
// pins sealed and erased anchors (ADR-0047).
func inlineCommentLog(t *testing.T) []Event {
	s := setup(t)
	page := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))
	noErr(t, s.CommitBody("anna", page.ID, Doc{Blocks: []Block{{Kind: Paragraph, Text: "Stop the writers first."}}}))
	c := mustComment(t)(s.Comment("anna", page.ID, NewComment{Text: "Which ones?", Anchor: &Anchor{Quote: "writers", Prefix: "Stop the ", Suffix: " first."}}))
	mustComment(t)(s.Comment("bert", page.ID, NewComment{Text: "All of them.", Reply: c.ID}))
	noErr(t, s.ResolveComment("anna", page.ID, c.ID, true))
	noErr(t, s.ResolveComment("bert", page.ID, c.ID, false))
	old := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Old"}))
	noErr(t, s.CommitBody("anna", old.ID, Doc{Blocks: []Block{{Kind: Paragraph, Text: "secret plan"}}}))
	mustComment(t)(s.Comment("anna", old.ID, NewComment{Text: "gone with the page", Anchor: &Anchor{Quote: "secret"}}))
	noErr(t, s.Trash("anna", old.ID))
	noErr(t, s.Purge("anna", old.ID))
	return s.Log()
}

// watchLog watches an item and a space and stops one watch (ADR-0046).
func watchLog(t *testing.T) []Event {
	s := setup(t)
	page := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))
	noErr(t, s.SetWatch("bert", "bert", page.ID, "", true))
	noErr(t, s.SetWatch("bert", "bert", "", "PLAT", true))
	noErr(t, s.SetWatch("bert", "bert", "", "PLAT", false))
	return s.Log()
}

// attachmentLog adds files, shows one as an image, deletes one and purges
// an item with a file, so the golden files pin sealed, deleted and erased
// attachments (ADR-0045).
func attachmentLog(t *testing.T) []Event {
	s := setup(t)
	page := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))
	file := func(id, name string) Attachment {
		return Attachment{ID: ID(id), Name: name, Type: "image/png", Size: 3, SHA256: strings.Repeat("ab", 32)}
	}
	if _, err := s.AddAttachment("anna", page.ID, file("0123456789abcdef0123456789abcdef", "diagram.png")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddAttachment("anna", page.ID, file("fedcba9876543210fedcba9876543210", "old.png")); err != nil {
		t.Fatal(err)
	}
	noErr(t, s.CommitBody("anna", page.ID, Doc{Blocks: []Block{{Kind: Image, Ref: "0123456789abcdef0123456789abcdef", Text: "The flow"}}}))
	noErr(t, s.DeleteAttachment("anna", page.ID, "fedcba9876543210fedcba9876543210"))
	old := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Old"}))
	if _, err := s.AddAttachment("anna", old.ID, file("00000000000000000000000000000001", "secret.pdf")); err != nil {
		t.Fatal(err)
	}
	noErr(t, s.Trash("anna", old.ID))
	noErr(t, s.Purge("anna", old.ID))
	return s.Log()
}

// labelLog labels items, then purges one, so the golden file pins sealed
// and erased labels (ADR-0044).
func labelLog(t *testing.T) []Event {
	s := setup(t)
	page := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))
	noErr(t, s.SetLabels("anna", page.ID, []string{"Ops", "backup"}))
	old := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Old"}))
	noErr(t, s.SetLabels("anna", old.ID, []string{"secret-project"}))
	noErr(t, s.Trash("anna", old.ID))
	noErr(t, s.Purge("anna", old.ID))
	return s.Log()
}

// blockKindsLog commits a body with every block kind of ADR-0042.
func blockKindsLog(t *testing.T) []Event {
	s := setup(t)
	page := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))
	noErr(t, s.CommitBody("anna", page.ID, Doc{Blocks: []Block{
		{Kind: TOC},
		{Kind: TaskItem, Text: "Back up", Checked: true},
		{Kind: TaskItem, Text: "Restore", Marks: []Mark{{Type: MarkBold, Start: 0, End: 7}}},
		{Kind: Panel, Tone: "warning", Text: "Stop the writers first."},
		{Kind: Table, Text: "Host\tRole\ndb1\tprimary"},
		{Kind: Children},
	}}))
	return s.Log()
}

// treeLog places pages among their siblings (ADR-0041), so the golden file
// pins the order an ItemMoved carries.
func treeLog(t *testing.T) []Event {
	s := setup(t)
	home := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Home"}))
	a := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "A", Parent: home.ID}))
	b := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "B", Parent: home.ID}))
	noErr(t, s.Place("anna", b.ID, "PLAT", home.ID, a.ID))
	noErr(t, s.Move("anna", a.ID, "PLAT", ""))
	return s.Log()
}

// commentLog adds, edits and deletes comments, then purges an item with a
// comment, so the golden file pins sealed and erased comment text
// (ADR-0035).
func commentLog(t *testing.T) []Event {
	s := setup(t)
	page := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))
	c := mustComment(t)(s.AddComment("anna", page.ID, "Looks good"))
	noErr(t, s.EditComment("anna", page.ID, c.ID, "Looks good to me"))
	noErr(t, s.DeleteComment("anna", page.ID, mustComment(t)(s.AddComment("bert", page.ID, "typo")).ID))
	old := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Old"}))
	mustComment(t)(s.AddComment("anna", old.ID, "gone with the page"))
	noErr(t, s.Trash("anna", old.ID))
	noErr(t, s.Purge("anna", old.ID))
	return s.Log()
}

// richTextLog commits bodies with every block kind and mark (ADR-0032).
func richTextLog(t *testing.T) []Event {
	s := setup(t)
	task := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "task", Title: "Patch", Properties: Props{"priority": "low"}}))
	page := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))
	noErr(t, s.CommitBody("anna", page.ID, richDoc(task.ID)))
	return s.Log()
}

func richDoc(mention ID) Doc {
	return Doc{Blocks: []Block{
		{Kind: Heading, Level: 2, Text: "Steps"},
		{Kind: Paragraph, Text: "Read the guide, then @task", Mentions: []ID{mention}, Marks: []Mark{
			{Type: MarkBold, Start: 0, End: 4},
			{Type: MarkLink, Start: 9, End: 14, Href: "https://example.org/guide"},
			{Type: MarkItalic, Start: 16, End: 20},
			{Type: MarkMention, Start: 21, End: 26, Ref: mention},
		}},
		{Kind: BulletItem, Text: "first", Marks: []Mark{{Type: MarkCode, Start: 0, End: 5}}},
		{Kind: OrderedItem, Text: "second"},
		{Kind: Quote, Text: "a quote"},
		{Kind: CodeBlock, Text: "go test ./..."},
		{Kind: Embed, Ref: mention},
	}}
}

func TestRichTextValidation(t *testing.T) {
	s := setup(t)
	task := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "task", Title: "Patch", Properties: Props{"priority": "low"}}))
	page := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))
	noErr(t, s.CommitBody("anna", page.ID, richDoc(task.ID)))
	got := mustItem(t)(s.Get(page.ID))
	if len(got.Body.Blocks[1].Marks) != 4 || got.Body.Blocks[0].Level != 2 {
		t.Fatalf("body %+v", got.Body.Blocks[:2])
	}
	if !hasRelation(s.Relations(page.ID, Outgoing), RelMentions, task.ID) {
		t.Fatal("a mention mark's item is not related")
	}
	bad := map[string]Block{
		"mark past the end":       {Kind: Paragraph, Text: "abc", Marks: []Mark{{Type: MarkBold, Start: 1, End: 4}}},
		"empty mark":              {Kind: Paragraph, Text: "abc", Marks: []Mark{{Type: MarkBold, Start: 2, End: 2}}},
		"negative start":          {Kind: Paragraph, Text: "abc", Marks: []Mark{{Type: MarkBold, Start: -1, End: 1}}},
		"unknown mark":            {Kind: Paragraph, Text: "abc", Marks: []Mark{{Type: "blink", Start: 0, End: 1}}},
		"javascript link":         {Kind: Paragraph, Text: "abc", Marks: []Mark{{Type: MarkLink, Start: 0, End: 1, Href: "javascript:alert(1)"}}},
		"relative link":           {Kind: Paragraph, Text: "abc", Marks: []Mark{{Type: MarkLink, Start: 0, End: 1, Href: "/admin"}}},
		"bold with a target":      {Kind: Paragraph, Text: "abc", Marks: []Mark{{Type: MarkBold, Start: 0, End: 1, Href: "https://x.org"}}},
		"mention not in mentions": {Kind: Paragraph, Text: "abc", Marks: []Mark{{Type: MarkMention, Start: 0, End: 1, Ref: task.ID}}},
		"marks in code":           {Kind: CodeBlock, Text: "abc", Marks: []Mark{{Type: MarkBold, Start: 0, End: 1}}},
		"level on a paragraph":    {Kind: Paragraph, Text: "abc", Level: 2},
		"level 4":                 {Kind: Heading, Text: "abc", Level: 4},
		"embed with text":         {Kind: Embed, Ref: task.ID, Text: "x"},
	}
	for name, b := range bad {
		if err := s.CommitBody("anna", page.ID, Doc{Blocks: []Block{b}}); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: err = %v, want ErrValidation", name, err)
		}
	}
	// Offsets count code points, not bytes: "é" is one character.
	noErr(t, s.CommitBody("anna", page.ID, Doc{Blocks: []Block{{Kind: Paragraph, Text: "café!", Marks: []Mark{{Type: MarkBold, Start: 3, End: 4}}}}}))
	for _, href := range []string{"http://x.org", "https://x.org/a?b", "mailto:a@x.org"} {
		if !SafeHref(href) {
			t.Errorf("%s refused", href)
		}
	}
	for _, href := range []string{"", "x", "https://", "mailto:", "data:text/html,x", "JAVASCRIPT:alert(1)", "%zz"} {
		if SafeHref(href) {
			t.Errorf("%s allowed", href)
		}
	}
}

func assertRebuilds(t *testing.T, golden string, sl Sealer, log []Event) {
	t.Helper()
	data, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []Event
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		batch, err := DecodeBatchSealed([]byte(line), sl)
		if err != nil {
			t.Fatalf("%s: %v", golden, err)
		}
		decoded = append(decoded, batch...)
	}
	r, err := Replay(decoded)
	if err != nil {
		t.Fatal(err)
	}
	live, err := Replay(log)
	if err != nil {
		t.Fatal(err)
	}
	all := Query{IncludeDeleted: true}
	if !reflect.DeepEqual(r.Find(all), live.Find(all)) {
		t.Fatalf("%s rebuilds a different state", golden)
	}
	if !reflect.DeepEqual(r.Log(), live.Log()) {
		t.Fatalf("%s rebuilds a different history", golden)
	}
}

func TestShreddedContentDecodesAsErased(t *testing.T) {
	log := everyKindLog(t)
	var created Event
	for _, e := range log {
		if e.Kind == ItemCreated && !e.Erased {
			created = e
			break
		}
	}
	data, err := EncodeBatchSealed([]Event{created}, fakeSealer{})
	noErr(t, err)
	if strings.Contains(string(data), created.Title) {
		t.Fatalf("sealed event shows its title: %s", data)
	}
	got, err := DecodeBatchSealed(data, fakeSealer{shredded: map[ID]bool{created.Item: true}})
	noErr(t, err)
	if !got[0].Erased || got[0].Title != "" || got[0].Props != nil {
		t.Fatalf("decoded %+v, want an erased event", got[0])
	}
	if _, err := DecodeBatch(data); err == nil {
		t.Fatal("sealed content decoded without keys")
	}
	if _, err := DecodeBatchSealed(data, failingSealer{}); err == nil {
		t.Fatal("a sealer error was ignored")
	}
	if _, err := EncodeBatchSealed([]Event{created}, failingSealer{}); err == nil {
		t.Fatal("a sealer error was ignored on encode")
	}
	bad := []byte(strings.Replace(string(data), `"sealed":"`, `"sealed":"QUJD`, 1))
	if _, err := DecodeBatchSealed(bad, fakeSealer{}); err == nil {
		t.Fatal("tampered sealed content decoded")
	}
}

type failingSealer struct{}

func (failingSealer) Seal(ID, []byte, []byte) ([]byte, error) { return nil, errors.New("no") }
func (failingSealer) Open(ID, []byte, []byte) ([]byte, error) { return nil, errors.New("no") }

var allEventKinds = []EventKind{
	TypeDefined, TypeRemoved, SpaceCreated, ItemCreated, TitleSet, PropertySet, Transitioned, ItemMoved,
	BodyCommitted, RelationAdded, RelationRemoved, ItemTrashed, ItemRestored, ItemPurged,
	ItemRedacted, GroupMemberAdded, RoleGranted, SystemRoleGranted, RestrictionSet,
	ItemImported, ItemReleased, CommentAdded, CommentEdited, CommentDeleted, CommentResolved, CommentReopened, RoleRevoked, GroupMemberRemoved, SystemRoleRevoked, LabelsSet,
	AttachmentAdded, AttachmentDeleted, Watched, Unwatched, HoldSet, HoldReleased, KanbanSet, KanbanRemoved, KanbanCardPlaced,
	SpaceTrashed, SpaceRestored, SpacePurged, SpaceRenamed,
	SecretSet, SecretRemoved, SecretRequested, SecretGranted, SecretDeclined, SecretErased, AttachmentLabelled,
	AttachmentRetentionSet,
}

// everyKindLog runs commands that produce every event kind, deterministically.
func everyKindLog(t *testing.T) []Event {
	s := setup(t)
	page := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Runbook"}))
	task := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "task", Title: "Patch", Properties: Props{"priority": "low"}}))
	noErr(t, s.SetTitle("anna", task.ID, "Patch the kernel"))
	noErr(t, s.SetProperty("anna", task.ID, "estimate", 2))
	noErr(t, s.SetProperty("anna", task.ID, "assignee", "bert"))
	noErr(t, s.Transition("anna", task.ID, "start"))
	noErr(t, s.CommitBody("anna", page.ID, Doc{Blocks: []Block{{Kind: Heading, Text: "Steps"}, {Kind: Paragraph, Text: "see", Mentions: []ID{task.ID}}}}))
	noErr(t, s.AddRelation("anna", "blocks", task.ID, page.ID))
	rel := s.Relations(task.ID, Outgoing)
	for _, r := range rel {
		if r.Type == "blocks" {
			noErr(t, s.RemoveRelation("anna", r.ID))
		}
	}
	noErr(t, s.CommitBody("anna", page.ID, Doc{}))
	other := mustItem(t)(s.Create("anna", NewItem{Space: "PLAT", Type: "page", Title: "Old"}))
	noErr(t, s.Move("anna", other.ID, "OPS", ""))
	noErr(t, s.Trash("anna", other.ID))
	noErr(t, s.Restore("anna", other.ID))
	noErr(t, s.Trash("anna", other.ID))
	noErr(t, s.Purge("anna", other.ID))
	noErr(t, s.AddToGroup("admin", "ops", "bert"))
	noErr(t, s.GrantRole("admin", "PLAT", "@ops", Editor))
	noErr(t, s.GrantSystemRole("admin", "carla", RoleErasure))
	noErr(t, s.Restrict("admin", page.ID, Restriction{Read: []string{"@ops"}}))
	noErr(t, s.Redact("carla", task.ID, "personal data"))
	_, _, err := s.Upsert("sync", Upsert{
		Origin: Origin{System: "jira", Instance: "acme", ExternalID: "1001", ExternalKey: "PLAT-90", Raw: "{}"},
		Space:  "PLAT", Type: "task", Title: "Imported", Properties: Props{"priority": "high"}, Key: "PLAT-90", Managed: "sync-1",
	})
	noErr(t, err)
	noErr(t, s.Release("sync", "sync-1", "PLAT"))
	return s.Log()
}
