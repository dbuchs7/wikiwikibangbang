package budget

import (
	"reflect"
	"strings"
	"testing"
)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

// Every field must have a positive default: a zero ceiling admits nothing,
// so a field added without a default would switch a feature off.
func TestEveryDefaultIsPositive(t *testing.T) {
	v := reflect.ValueOf(Default())
	for i := range v.NumField() {
		if v.Field(i).Int() <= 0 {
			t.Errorf("%s has no positive default", v.Type().Field(i).Name)
		}
	}
}

func TestEnvVarNames(t *testing.T) {
	for field, want := range map[string]string{
		"RequestBody":      "WWBB_BUDGET_REQUEST_BODY",
		"SearchQueryBytes": "WWBB_BUDGET_SEARCH_QUERY_BYTES",
		"SSOPending":       "WWBB_BUDGET_SSO_PENDING",
	} {
		if got := EnvVar(field); got != want {
			t.Errorf("EnvVar(%q) = %q, want %q", field, got, want)
		}
	}
}

func TestEveryFieldIsConfigurable(t *testing.T) {
	vars := map[string]string{}
	for _, name := range Names() {
		vars[EnvVar(name)] = "7"
	}
	b, problems := FromEnv(env(vars))
	if len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}
	v := reflect.ValueOf(b)
	for i := range v.NumField() {
		if v.Field(i).Int() != 7 {
			t.Errorf("%s = %d, want 7 from the environment", v.Type().Field(i).Name, v.Field(i).Int())
		}
	}
}

// A typo in a deployment's environment must not remove a ceiling: anything
// that is not a positive number in range keeps the default and is reported.
func TestBadValuesKeepTheDefault(t *testing.T) {
	b, problems := FromEnv(env(map[string]string{
		"WWBB_BUDGET_REQUEST_BODY":       "64k",
		"WWBB_BUDGET_PAGE_ITEMS":         "0",
		"WWBB_BUDGET_LIVE_SESSIONS":      "-5",
		"WWBB_BUDGET_LIVE_CLIENTS":       "99999999999999999999",
		"WWBB_BUDGET_SEARCH_QUERY_TERMS": "  ",
	}))
	if b != Default() {
		t.Errorf("bad values changed the budgets: %+v", b)
	}
	if len(problems) != 4 {
		t.Fatalf("want 4 problems (an empty value is unset), got %d: %v", len(problems), problems)
	}
	for _, p := range problems {
		if !strings.Contains(p, "keeping the default") {
			t.Errorf("problem does not say what happens: %q", p)
		}
	}
}

func TestValueIsTrimmed(t *testing.T) {
	b, problems := FromEnv(env(map[string]string{"WWBB_BUDGET_PAGE_ITEMS": " 50 "}))
	if len(problems) != 0 || b.PageItems != 50 {
		t.Fatalf("got %d, problems %v", b.PageItems, problems)
	}
}

func TestGetReturnsTheDefaultUntilSet(t *testing.T) {
	Set(Default())
	if Get() != Default() {
		t.Fatal("Get after Set(Default()) differs from Default()")
	}
	b := Default()
	b.PageItems = 3
	Set(b)
	t.Cleanup(func() { Set(Default()) })
	if Get().PageItems != 3 {
		t.Fatalf("Get().PageItems = %d after Set, want 3", Get().PageItems)
	}
}

func TestChangedListsOnlyWhatDiffers(t *testing.T) {
	b := Default()
	b.LiveClients = 12
	got := b.Changed()
	if len(got) != 1 || got[0] != "WWBB_BUDGET_LIVE_CLIENTS=12" {
		t.Fatalf("Changed() = %v", got)
	}
	if len(Default().Changed()) != 0 {
		t.Fatal("the defaults report changes")
	}
}
