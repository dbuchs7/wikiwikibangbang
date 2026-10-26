package connector

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/dbuchs7/wikiwikibangbang/internal/item"
	"github.com/dbuchs7/wikiwikibangbang/internal/site"
)

// maxPages bounds how many pages of a list a JSON source is read in, so
// a misbehaving source cannot keep a run going; one answer is bounded by
// maxAnswer, as for CKAN.
const maxPages = 50

// jsonReader reads any source that answers JSON over http (ADR-0127):
// a list of entries at a path of the answer, each with an ID, a title,
// a link, a text and further values at paths of its own. Its defaults
// read the connectors' own feed format.
type jsonReader struct{}

func (jsonReader) Read(ctx context.Context, c site.Connector, env Env) (reading, error) {
	var rd reading
	j := c.JSON
	next, seen := j.URL, map[string]bool{}
	for page := 0; next != "" && page < maxPages; page++ {
		answer, err := getJSON(ctx, env, next)
		if err != nil {
			return rd, err
		}
		list, ok := at(answer, j.Items).([]any)
		if !ok {
			return rd, fmt.Errorf("connector: the answer of %s has no list at %q", next, cmp.Or(j.Items, "(the answer itself)"))
		}
		rd.datasets += len(list)
		for i, v := range list {
			e, err := jsonEntry(v, j, c.Name)
			switch {
			case err != nil:
				rd.problem("entry %d of %s: %v", i+1, next, err)
			case seen[e.SourceID]:
				rd.problem("%s: the ID %q is given twice; the second is left out", e.Title, e.SourceID)
			default:
				seen[e.SourceID] = true
				rd.entries = append(rd.entries, e)
			}
		}
		next = ""
		if j.Next != "" {
			if n, _ := at(answer, j.Next).(string); n != "" {
				if !webLink(n) {
					rd.problem("the address of the next page, %q, is not an http or https address; reading stops", n)
					break
				}
				next = n
			}
		}
		if page == maxPages-1 && next != "" {
			rd.problem("the list has more than %d pages; the rest is not read", maxPages)
		}
	}
	rd.apply = true
	return rd, nil
}

// getJSON fetches and decodes one answer, sending the credential as a
// bearer token if one is set.
func getJSON(ctx context.Context, env Env, addr string) (any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if env.Token != "" {
		req.Header.Set("Authorization", "Bearer "+env.Token)
	}
	resp, err := env.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("connector: %s answered %s", addr, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxAnswer {
		return nil, fmt.Errorf("connector: the answer of %s is larger than %d MB", addr, maxAnswer>>20)
	}
	var v any
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("connector: the answer of %s is no JSON: %w", addr, err)
	}
	return v, nil
}

// at follows a path of keys and list positions joined by dots; an empty
// path is v itself. It returns nil where the path leads nowhere.
func at(v any, path string) any {
	if path == "" {
		return v
	}
	for part := range strings.SplitSeq(path, ".") {
		switch x := v.(type) {
		case map[string]any:
			v = x[part]
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(x) {
				return nil
			}
			v = x[i]
		default:
			return nil
		}
	}
	return v
}

// text is a value as text: a string, number or truth value as written,
// a list as its texts joined by commas, anything else as nothing.
func text(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case json.Number:
		return x.String()
	case bool:
		return strconv.FormatBool(x)
	case []any:
		var parts []string
		for _, e := range x {
			if t := text(e); t != "" {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, ", ")
	}
	return ""
}

var errNoID = errors.New("it has no ID")

// jsonEntry makes an entry of one element of the list: its title, a
// panel saying where the page comes from, its text, a link to it in the
// source, and a table of its further values, which Fields maps from.
func jsonEntry(v any, j *site.JSONSource, source string) (Entry, error) {
	if _, ok := v.(map[string]any); !ok {
		return Entry{}, errors.New("it is not an object")
	}
	e := Entry{SourceID: text(at(v, j.ID)), Title: oneLine(text(at(v, j.Title)))}
	switch {
	case e.SourceID == "":
		return e, errNoID
	case len(e.SourceID) > 200:
		return e, fmt.Errorf("its ID is longer than 200 characters")
	}
	e.Title = cmp.Or(e.Title, e.SourceID)
	e.Dataset = e.SourceID
	if l := text(at(v, j.Link)); j.Link != "" && webLink(l) && item.SafeHref(l) {
		e.DatasetURL = l
	}
	e.Values = map[string]string{}
	for _, name := range slices.Sorted(maps.Keys(j.Values)) {
		if t := text(at(v, j.Values[name])); t != "" {
			e.Values[name] = t
		}
	}
	w := wordsIn(cmp.Or(j.Language, "de"))
	var b doc
	b.blocks = append(b.blocks, item.Block{Kind: item.Panel, Tone: "info", Text: fmt.Sprintf(w.generated, source)})
	if j.Text != "" {
		b.paragraphs(text(at(v, j.Text)))
	}
	if e.DatasetURL != "" {
		b.links([][2]string{{w.entry, e.DatasetURL}})
	}
	var facts [][2]string
	for _, name := range slices.Sorted(maps.Keys(e.Values)) {
		facts = append(facts, [2]string{name, e.Values[name]})
	}
	b.table(w, facts)
	e.Body = b.blocks
	return e, nil
}
