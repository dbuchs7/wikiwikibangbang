package migrate

import (
	"slices"
	"testing"
)

func TestADFTextKeepsTheTextOfEveryBlock(t *testing.T) {
	doc := `{"type":"doc","version":1,"content":[
	 {"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Steps"}]},
	 {"type":"paragraph","content":[{"type":"text","text":"Ask "},{"type":"mention","attrs":{"id":"acc-anna","text":"@Anna"}},
	   {"type":"text","text":" first, see "},{"type":"text","text":"the runbook","marks":[{"type":"link","attrs":{"href":"https://wiki.example/rb"}}]},
	   {"type":"hardBreak"},{"type":"text","text":"due "},{"type":"date","attrs":{"timestamp":"1767225600000"}}]},
	 {"type":"orderedList","attrs":{"order":3},"content":[
	   {"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"restart"}]},
	     {"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"twice"}]}]}]}]},
	   {"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"check"}]}]}]},
	 {"type":"codeBlock","content":[{"type":"text","text":"make test"}]},
	 {"type":"table","content":[{"type":"tableRow","content":[
	   {"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"Host"}]}]},
	   {"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"db1"}]}]}]}]},
	 {"type":"taskList","content":[{"type":"taskItem","attrs":{"state":"DONE"},"content":[{"type":"text","text":"backup"}]}]},
	 {"type":"rule"},
	 {"type":"mediaSingle","content":[{"type":"media","attrs":{"id":"x"}}]}
	]}`
	text, dropped := ADFText([]byte(doc))
	want := "Steps\n\n" +
		"Ask @Anna first, see the runbook (https://wiki.example/rb)\ndue 2026-01-01\n\n" +
		"3. restart\n\n   - twice\n\n4. check\n\n" +
		"make test\n\n" +
		"Host | db1\n\n" +
		"[x] backup\n\n" +
		"[attachment]"
	if text != want {
		t.Errorf("got\n%q\nwant\n%q", text, want)
	}
	if !slices.Equal(dropped, []string{"attachments"}) {
		t.Errorf("dropped %v", dropped)
	}
	for _, empty := range []string{"", "null", "not json", `{"type":"doc","content":[]}`} {
		if text, _ := ADFText([]byte(empty)); text != "" {
			t.Errorf("%q gave %q", empty, text)
		}
	}
}
