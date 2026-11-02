package server

import (
	"strings"

	"github.com/dbuchs7/orbithek/internal/auth"
	"github.com/dbuchs7/orbithek/internal/item"
)

// accessTexts word the task of a request to read protected content and the
// comments that record its answer (ADR-0162), in the language of the one
// who reads them: the task in the language of the one it is assigned to,
// else of the one who asks; a comment in the language of the one who
// answers. People are named by their usernames. Content is not translated
// afterwards: the words are the item's, as anyone would have typed them.
type accessTexts struct{ accounts *auth.Accounts }

func (a accessTexts) name(id string) string {
	if n := a.accounts.Usernames([]auth.UserID{auth.UserID(id)})[auth.UserID(id)]; n != "" {
		return n
	}
	return id
}

func (a accessTexts) lang(ids ...string) string {
	for _, id := range ids {
		if l := a.accounts.Language(auth.UserID(id)); l != "" {
			return l
		}
	}
	return "en"
}

func (a accessTexts) Task(t item.AccessTask) (string, item.Doc) {
	lang := a.lang(t.Assignee, t.Requester)
	requester, assignee := a.name(t.Requester), a.name(t.Assignee)
	what := text(lang, "access.passage", t.Number)
	if t.Blocks {
		what = text(lang, "access.blocks", t.Number)
	}
	date := t.At.Format("2006-01-02")
	if base, _, _ := strings.Cut(lang, "-"); base == "de" {
		date = t.At.Format("02.01.2006")
	}
	who := text(lang, "access.protector", assignee)
	if t.Owner {
		who = text(lang, "access.owner", assignee)
	}
	return text(lang, "access.title", requester, t.Title), item.Doc{Blocks: []item.Block{
		item.MentionParagraph(text(lang, "access.asked", requester, date, what), t.Item, t.Title, text(lang, "access.askedEnd")),
		{Kind: item.Paragraph, Text: who},
		{Kind: item.Paragraph, Text: text(lang, "access.hidden")},
		{Kind: item.Paragraph, Text: text(lang, "access.how", requester)},
	}}
}

func (a accessTexts) Outcome(o item.AccessOutcome, actor string, _ item.AccessTask) string {
	key := map[item.AccessOutcome]string{item.AccessGranted: "access.granted", item.AccessDeclined: "access.declined",
		item.AccessClosed: "access.closed", item.AccessRemoved: "access.removed"}[o]
	return text(a.lang(actor), key, a.name(actor))
}
