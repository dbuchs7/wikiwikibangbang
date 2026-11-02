package item

import (
	"maps"
	"slices"
)

// Protected blocks (ADR-0160): a Secret may protect whole blocks of a body
// instead of a passage of one block's text — a heading, a picture, a
// table, an embed, a diagram, or a section of several blocks at once. Each
// protected block names the Secret in Block.Secret; the Secret itself is
// the one of protected passages (ADR-0088), so who reads, asks and decides
// is the same. A Secret protects either passages or blocks, never both.
//
// For everyone else, each run of consecutive blocks of a Secret they may
// not read is one placeholder block: a paragraph whose text is
// SecretPlaceholder and whose Secret names the protection. It tells that
// something is protected there and who decides, and nothing of the kind,
// number, size, text, file or target of the blocks.
//
// A file a protected block shows — an image, a drawing, a video, a BPMN
// model — is protected with it, every version of its stack included
// (ADR-0103): whoever may not read the Secret gets the file nowhere. The
// protection sticks to the file until the Secret is taken off or erased,
// even when the block leaves the text, so editing never opens a file by
// accident (Item.secretFiles).

// showsFile reports whether a block kind shows a file of its item in Ref.
func showsFile(k BlockKind) bool { return k == Image || k == Video || k == BPMN }

// blockSecrets returns the Secrets the blocks of a body name.
func blockSecrets(d Doc) map[ID]bool {
	var out map[ID]bool
	for _, b := range d.Blocks {
		if b.Secret != "" {
			if out == nil {
				out = map[ID]bool{}
			}
			out[b.Secret] = true
		}
	}
	return out
}

// protectsBlocks reports whether a Secret of an item protects blocks of
// its body rather than a passage.
func (it *Item) protectsBlocks(id ID) bool {
	return slices.ContainsFunc(it.Body.Blocks, func(b Block) bool { return b.Secret == id })
}

// refSecrets returns, for each relation a body implies ("type/to") only
// from protected blocks, the Secrets of those blocks in the order of the
// body. A relation that an unprotected block implies too is not in it:
// everyone may learn of that one.
func refSecrets(d Doc) map[string][]ID {
	open := map[string]bool{}
	var out map[string][]ID
	for _, b := range d.Blocks {
		var keys []string
		if b.Kind == Embed {
			keys = append(keys, RelEmbeds+"/"+string(b.Ref))
		}
		for _, m := range b.Mentions {
			keys = append(keys, RelMentions+"/"+string(m))
		}
		for _, k := range keys {
			switch {
			case b.Secret == "":
				open[k] = true
			case !slices.Contains(out[k], b.Secret):
				if out == nil {
					out = map[string][]ID{}
				}
				out[k] = append(out[k], b.Secret)
			}
		}
	}
	for k := range open {
		delete(out, k)
	}
	return out
}

// noteSecretFiles remembers the files a committed body shows in protected
// blocks; it runs live and on replay. A file stays protected with the
// first Secret that showed it until that Secret is taken off or erased.
func (it *Item) noteSecretFiles(d Doc) {
	for _, b := range d.Blocks {
		if b.Secret == "" || b.Ref == "" || !showsFile(b.Kind) {
			continue
		}
		if _, ok := it.secretFiles[b.Ref]; ok {
			continue
		}
		it.secretFiles = maps.Clone(it.secretFiles)
		if it.secretFiles == nil {
			it.secretFiles = map[ID]ID{}
		}
		it.secretFiles[b.Ref] = b.Secret
	}
}

// forgetSecretFiles drops what a Secret protected of the item's files; it
// runs live and on replay when the Secret is taken off or erased.
func (it *Item) forgetSecretFiles(secret ID) {
	if len(it.secretFiles) == 0 {
		return
	}
	it.secretFiles = maps.Clone(it.secretFiles)
	maps.DeleteFunc(it.secretFiles, func(_, s ID) bool { return s == secret })
	if len(it.secretFiles) == 0 {
		it.secretFiles = nil
	}
}

// stackMembers returns every file in the stack of versions of a file
// (ADR-0103): those that share its first version.
func stackMembers(it *Item, file ID) []ID {
	a, ok := it.attachment(file)
	if !ok {
		return []ID{file}
	}
	first := firstVersion(it, *a)
	var out []ID
	for _, o := range it.Attachments {
		if firstVersion(it, o) == first {
			out = append(out, o.ID)
		}
	}
	return out
}

// protectedFiles maps each protected file of an item, every version of a
// protected stack included, to the Secret that protects it, as files and
// secrets are given (the item's own, or those of a trial).
func protectedFiles(it *Item, secretFiles map[ID]ID) map[ID]ID {
	if len(secretFiles) == 0 {
		return nil
	}
	out := map[ID]ID{}
	// In the order the files were added, so the first protection wins.
	for _, a := range it.Attachments {
		sec, ok := secretFiles[a.ID]
		if !ok {
			continue
		}
		for _, f := range stackMembers(it, a.ID) {
			if _, taken := out[f]; !taken {
				out[f] = sec
			}
		}
	}
	return out
}

// fileSecret returns the Secret that protects a file of an item, if any.
func (it *Item) fileSecret(file ID) (ID, bool) {
	sec, ok := protectedFiles(it, it.secretFiles)[file]
	return sec, ok
}

// readsFile reports whether the principal may have a file of an item they
// may read: it is protected by no Secret, or by one they read (ADR-0160).
func (s *Store) readsFile(principal string, it *Item, file ID) bool {
	sec, ok := it.fileSecret(file)
	if !ok {
		return true
	}
	i, ok := it.secret(sec)
	return ok && s.readsSecret(principal, it.Secrets[i])
}

// checkSecretFiles checks that a body keeps every protected file in blocks
// of its protection: a file a protected block shows, and the other
// versions of its stack, are shown by no unprotected block and no block of
// another protection. Otherwise a file would be veiled in one place and
// shown in another.
func checkSecretFiles(it *Item, d Doc) error {
	trial := maps.Clone(it.secretFiles)
	for _, b := range d.Blocks {
		if b.Secret == "" || b.Ref == "" || !showsFile(b.Kind) {
			continue
		}
		if _, ok := trial[b.Ref]; !ok {
			if trial == nil {
				trial = map[ID]ID{}
			}
			trial[b.Ref] = b.Secret
		}
	}
	prot := protectedFiles(it, trial)
	for i, b := range d.Blocks {
		if b.Ref == "" || !showsFile(b.Kind) {
			continue
		}
		sec, ok := prot[b.Ref]
		if !ok && b.Secret != "" {
			// A file the item does not have yet is checked by CommitBody;
			// it is protected by this block alone.
			continue
		}
		if sec != b.Secret {
			if b.Secret == "" {
				return wrapf(invalid("this file is protected in another place of the text; show it there, or protect this block with the same protection"), "block %d", i+1)
			}
			return wrapf(invalid("this file is protected by another protection of the text"), "block %d", i+1)
		}
	}
	return nil
}

// veilBlocks replaces each run of consecutive blocks of a Secret in hide
// by one placeholder block (ADR-0160).
func veilBlocks(d Doc, hide map[ID]bool) Doc {
	if !slices.ContainsFunc(d.Blocks, func(b Block) bool { return b.Secret != "" && hide[b.Secret] }) {
		return d
	}
	out := make([]Block, 0, len(d.Blocks))
	for _, b := range d.Blocks {
		if b.Secret == "" || !hide[b.Secret] {
			out = append(out, b)
			continue
		}
		// A block of a hidden Secret right after the placeholder of the
		// same Secret belongs to it: one placeholder, whatever the number.
		if n := len(out); n > 0 && out[n-1].Secret == b.Secret {
			continue
		}
		out = append(out, Block{Kind: Paragraph, Text: SecretPlaceholder, Secret: b.Secret})
	}
	return Doc{Blocks: out}
}

// veilFiles drops the files of an item the principal may not have, so
// what is read of the item does not even count them.
func (s *Store) veilFiles(principal string, it *Item) []Attachment {
	if len(it.secretFiles) == 0 {
		return it.Attachments
	}
	return slices.DeleteFunc(slices.Clone(it.Attachments), func(a Attachment) bool { return !s.readsFile(principal, it, a.ID) })
}

// seesRelation reports whether the principal may learn of a relation: one
// derived only from protected blocks is known to those who read one of
// them (ADR-0160).
func (x *Session) seesRelation(r *Relation) bool {
	if len(r.secrets) == 0 {
		return true
	}
	it := x.s.items[r.From]
	for _, id := range r.secrets {
		if i, ok := it.secret(id); ok && x.s.readsSecret(x.who, it.Secrets[i]) {
			return true
		}
	}
	return false
}

// visibleRelations are the store's relations as they were made, without
// those the principal may not learn of.
func (x *Session) visibleRelations() []*Relation {
	return slices.DeleteFunc(x.s.sortedRelations(), func(r *Relation) bool { return !x.seesRelation(r) })
}

// ProtectBlocks protects the blocks from to to (counted from 0, both
// included) of an item's body with a new Secret: readers read them besides
// the one who protects them. Who may edit the item and reads all of its
// protections may (ADR-0160).
func (x *Session) ProtectBlocks(id ID, from, to int, readers []string) (Secret, error) {
	it, err := x.editableBody(id)
	if err != nil {
		return Secret{}, err
	}
	if from < 0 || to < from || to >= len(it.Body.Blocks) {
		return Secret{}, invalid("the body has no blocks %d to %d", from+1, to+1)
	}
	readers, err = checkReaders(readers)
	if err != nil {
		return Secret{}, err
	}
	body := it.Body.clone()
	sec := Secret{ID: x.s.newID(), By: x.who, Readers: readers}
	for i := from; i <= to; i++ {
		b := body.Blocks[i]
		if b.Secret != "" {
			return Secret{}, wrapf(invalid("the block is protected already"), "block %d", i+1)
		}
		if slices.ContainsFunc(b.Marks, func(m Mark) bool { return m.Type == MarkSecret }) {
			return Secret{}, wrapf(invalid("the block holds a protected passage; take its protection off first"), "block %d", i+1)
		}
		b.Secret = sec.ID
		body.Blocks[i] = b
	}
	trial := it.clone()
	trial.Secrets = append(trial.Secrets, sec)
	if err := checkSecrets(&trial, body); err != nil {
		return Secret{}, err
	}
	return sec, x.s.recordBatch(x.who, Event{Kind: SecretSet, Item: id, Secret: &sec}, Event{Kind: BodyCommitted, Item: id, Body: &body})
}

// refersVisibly reports whether the principal may learn of a reference
// from an item into the subtree of root.
func (x *Session) refersVisibly(from, root ID) bool {
	in := map[ID]bool{}
	for _, m := range x.s.subtree(root) {
		in[m] = true
	}
	for _, r := range x.s.rels {
		if r.From == from && in[r.To] && x.seesRelation(r) {
			return true
		}
	}
	return false
}
