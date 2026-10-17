package item

import (
	"errors"
	"flag"
	"math"
	"os"
	"path/filepath"
	"reflect"
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
		"newer format":       `[{"v":2,"seq":1,"at":"2026-01-01T00:00:00Z","kind":"SpaceCreated"}]`,
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

// TestPersistedFormatIsStable pins the format of a log covering every event
// kind. A failure means old data directories would be read differently:
// change the format only together with CodecVersion and a migration, then
// regenerate with `go test ./internal/item -run PersistedFormat -update`.
func TestPersistedFormatIsStable(t *testing.T) {
	log := everyKindLog(t)
	var lines []string
	kinds := map[EventKind]bool{}
	for _, e := range log {
		data, err := EncodeBatch([]Event{e})
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, string(data))
		kinds[e.Kind] = true
	}
	for _, k := range allEventKinds {
		if !kinds[k] {
			t.Errorf("the golden log has no %s event; extend everyKindLog", k)
		}
	}
	got := strings.Join(lines, "\n") + "\n"

	golden := filepath.Join("testdata", "events.golden.jsonl")
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
		t.Fatalf("the persisted format changed; see the comment on this test\n got: %s\nwant: %s", got, want)
	}

	// The golden file itself must still decode into the same events.
	var decoded []Event
	for _, line := range strings.Split(strings.TrimSpace(string(want)), "\n") {
		batch, err := DecodeBatch([]byte(line))
		if err != nil {
			t.Fatal(err)
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
		t.Fatal("the golden log rebuilds a different state")
	}
}

var allEventKinds = []EventKind{
	TypeDefined, SpaceCreated, ItemCreated, TitleSet, PropertySet, Transitioned, ItemMoved,
	BodyCommitted, RelationAdded, RelationRemoved, ItemTrashed, ItemRestored, ItemPurged,
	ItemRedacted, GroupMemberAdded, RoleGranted, SystemRoleGranted, RestrictionSet,
	ItemImported, ItemReleased,
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
