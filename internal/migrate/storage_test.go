package migrate

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
)

type fakeResolver struct {
	pages  map[string]item.ID // "SPACE/Title"
	issues map[string]item.ID // key
	users  map[string]string  // account id -> display
}

func (f fakeResolver) Page(space, title string) (item.ID, bool) {
	id, ok := f.pages[space+"/"+title]
	return id, ok
}
func (f fakeResolver) Issue(key string) (item.ID, bool) { id, ok := f.issues[key]; return id, ok }
func (f fakeResolver) User(account string) (string, bool) {
	n, ok := f.users[account]
	return n, ok
}

func TestConvertStorage(t *testing.T) {
	r := fakeResolver{
		pages:  map[string]item.ID{"PLAT/Runbook": "page-1"},
		issues: map[string]item.ID{"PLAT-7": "issue-7"},
		users:  map[string]string{"acc-1": "anna"},
	}
	src := `<h2>Actions</h2>` +
		`<p>See&nbsp;<strong>the</strong> <ac:link><ri:page ri:content-title="Runbook" ri:space-key="PLAT"/></ac:link>, ask <ac:link><ri:user ri:account-id="acc-1"/></ac:link>.</p>` +
		`<ac:structured-macro ac:name="jira"><ac:parameter ac:name="key">PLAT-7</ac:parameter></ac:structured-macro>` +
		`<ul><li>first</li><li>second</li></ul>` +
		`<ac:structured-macro ac:name="info"><ac:rich-text-body><p>Note</p></ac:rich-text-body></ac:structured-macro>` +
		`<table><tr><td>a</td></tr></table>` +
		`<p>Missing <ac:link><ri:page ri:content-title="Gone" ri:space-key="PLAT"/></ac:link> and <ac:structured-macro ac:name="jira"><ac:parameter ac:name="key">PLAT-99</ac:parameter></ac:structured-macro></p>`

	doc, problems := ConvertStorage(src, "PLAT", r)
	got := []string{}
	for _, b := range doc.Blocks {
		got = append(got, string(b.Kind)+":"+b.Text+":"+string(b.Ref)+":"+join(b.Mentions))
	}
	want := []string{
		"heading:Actions::",
		"paragraph:See the Runbook, ask @anna.::page-1",
		"embed::issue-7:",
		"bullet:first::",
		"bullet:second::",
		`unsupported:<ac:structured-macro ac:name="info"><ac:rich-text-body><p>Note</p></ac:rich-text-body></ac:structured-macro>::`,
		"unsupported:<table><tr><td>a</td></tr></table>::",
		"paragraph:Missing Gone and::",
		"paragraph:[Jira issue PLAT-99]::",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("blocks:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if len(problems) != 2 || !strings.Contains(problems[0], "Gone") || !strings.Contains(problems[1], "PLAT-99") {
		t.Fatalf("problems = %q", problems)
	}
}

func TestConvertStorageUnknownUserAndMalformed(t *testing.T) {
	doc, problems := ConvertStorage(`<p>Hi <ac:link><ri:user ri:account-id="x"/></ac:link></p>`, "PLAT", fakeResolver{})
	if doc.Blocks[0].Text != "Hi @unknown-user" || len(problems) != 1 {
		t.Fatalf("doc=%+v problems=%q", doc, problems)
	}
	// Unclosed inline tags are recovered without loss, as in HTML.
	doc, problems = ConvertStorage(`<p>line one<br>line <strong>two</p>`, "PLAT", fakeResolver{})
	if len(doc.Blocks) != 1 || doc.Blocks[0].Text != "line one\nline two" || len(problems) != 0 {
		t.Fatalf("recoverable markup: %+v %q", doc, problems)
	}
	// A body cut off inside a tag keeps the text before it and reports a problem.
	doc, problems = ConvertStorage(`<p>kept</p><p class="x`, "PLAT", fakeResolver{})
	if len(doc.Blocks) != 1 || doc.Blocks[0].Text != "kept" || len(problems) != 1 {
		t.Fatalf("broken markup: %+v %q", doc, problems)
	}
	if doc, problems := ConvertStorage("", "PLAT", fakeResolver{}); len(doc.Blocks) != 0 || len(problems) != 0 {
		t.Fatalf("empty body: %+v %q", doc, problems)
	}
}

func join(ids []item.ID) string {
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = string(id)
	}
	return strings.Join(s, ",")
}

func TestConvertStorageRichBlocks(t *testing.T) {
	src := `<h2>Two</h2><h5>Five</h5><blockquote>quoted</blockquote><ol><li><p>one</p></li><li>two<ul><li>inner</li></ul></li></ol>`
	doc, problems := ConvertStorage(src, "PLAT", nil)
	var got []string
	for _, b := range doc.Blocks {
		got = append(got, fmt.Sprintf("%s/%d:%s", b.Kind, b.Level, b.Text))
	}
	want := "heading/2:Two heading/3:Five quote/0:quoted ordered/0:one ordered/0:two bullet/0:inner"
	if strings.Join(got, " ") != want || len(problems) != 0 {
		t.Fatalf("got %s (problems %v), want %s", strings.Join(got, " "), problems, want)
	}
}
