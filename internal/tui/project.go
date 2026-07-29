package tui

import (
	"sort"
	"strings"

	"github.com/pottom/harmos/internal/edit"
	"github.com/pottom/harmos/internal/secret"
	"github.com/pottom/harmos/internal/vault"
)

// The staged projection.
//
// The tree and the entry table are built from what the file says. That was fine
// while harmos only read, and wrong the moment it could stage: a folder you had
// just created did not exist anywhere you could put anything, an entry you had
// just created was nowhere but the Changes tab, a renamed folder still showed
// its old name, and a moved entry still sat in the folder it had left. Every one
// of those is the interface disagreeing with itself about the same vault.
//
// So the read model is projected through the staged set before the tree is
// built: it shows the vault as it will be, with each change marked in place.
//
// Deletions are the exception, and deliberately: they stay, marked, because what
// you are about to remove is the thing you most need to see before you approve
// it. Removing them from the tree would remove them from review.
func project(entries []vault.Entry, folders []vault.Folder, set edit.Set) ([]vault.Entry, []vault.Folder) {
	if set.Empty() {
		return entries, folders
	}

	outE := append([]vault.Entry(nil), entries...)
	outF := append([]vault.Folder(nil), folders...)

	pathOf := func(id string) (string, string, bool) { // path, source
		for _, f := range outF {
			if f.ID == id {
				return f.Path, f.Source, true
			}
		}
		return "", "", false
	}

	// Creations first: everything after this can then refer to them.
	for _, op := range set.Effective() {
		switch op.Kind {
		case edit.CreateGroup:
			parent, _, ok := pathOf(op.Parent)
			path := op.Name
			if ok && parent != "" {
				path = parent + "/" + op.Name
			}
			outF = append(outF, vault.Folder{
				ID: op.Target, ParentID: op.Parent, Source: op.Source, Path: path, Name: op.Name,
			})
		case edit.CreateEntry:
			if op.After == nil {
				continue
			}
			path, _, _ := pathOf(op.After.GroupID)
			if op.Parent != "" {
				if p, _, ok := pathOf(op.Parent); ok {
					path = p
				}
			}
			outE = append(outE, entryFromDraft(op.Source, path, *op.After))
		}
	}

	// Then the changes that move names around. A rename or a move of a folder
	// takes its whole subtree with it, so the descendants' paths are rewritten
	// too — otherwise the children would still be filed under a path nothing
	// leads to any more.
	for _, op := range set.Effective() {
		switch op.Kind {
		case edit.EditEntry:
			if op.After != nil {
				for i := range outE {
					if outE[i].ID != op.Target {
						continue
					}
					// An edit changes fields, never location. The draft carries a
					// group because it was loaded with one, and if that load
					// predates a staged move, taking it here would quietly undo
					// the move.
					where, group := outE[i].Path, outE[i].GroupID
					outE[i] = entryFromDraft(op.Source, where, *op.After)
					outE[i].GroupID = group
				}
			}
		case edit.RenameGroup:
			from, source, ok := pathOf(op.Target)
			if !ok {
				continue
			}
			to := op.Name
			if parent := parentPath(from); parent != "" {
				to = parent + "/" + op.Name
			}
			outE, outF = reparent(outE, outF, source, from, to, op.Target, "")
		case edit.MoveGroup:
			from, source, ok := pathOf(op.Target)
			if !ok {
				continue
			}
			dest, _, _ := pathOf(op.Parent)
			name := from
			if i := strings.LastIndex(from, "/"); i >= 0 {
				name = from[i+1:]
			}
			to := name
			if dest != "" {
				to = dest + "/" + name
			}
			outE, outF = reparent(outE, outF, source, from, to, op.Target, op.Parent)
		case edit.MoveEntry:
			dest, _, ok := pathOf(op.Parent)
			if !ok {
				continue
			}
			for i := range outE {
				if outE[i].ID == op.Target {
					outE[i].Path, outE[i].GroupID = dest, op.Parent
				}
			}
		}
	}

	sort.SliceStable(outF, func(i, j int) bool { return outF[i].Path < outF[j].Path })
	return outE, outF
}

// entryFromDraft is the read-model view of something staged.
//
// It is the lossy projection on purpose — the same one the reader sees for
// everything else in the tree — and it is never a source for a write: the write
// applies the draft, not this.
func entryFromDraft(source, path string, d edit.Draft) vault.Entry {
	// Sanitised the same way the reader sanitises what it takes off the file:
	// these rows are rendered by the same code, and a staged value is no more
	// trustworthy than a stored one. It happened to hold anyway — the text
	// widget strips control runes on the way in — but by accident rather than
	// by construction, and the editor is not the only way a draft can be built.
	e := vault.Entry{
		ID: d.ID, GroupID: d.GroupID, Source: source, Path: path,
		Title: vault.OneLine(d.Title), Username: vault.OneLine(d.Username),
		URL: vault.OneLine(d.URL), Notes: vault.MultiLine(d.Notes),
		Password: d.Password, TOTP: vault.OneLine(d.TOTP),
	}
	if e.Password.Reveal() == "" {
		e.Password = secret.New("")
	}
	for _, t := range strings.FieldsFunc(d.Tags, func(r rune) bool { return r == ';' || r == ',' }) {
		if t = strings.TrimSpace(t); t != "" {
			e.Tags = append(e.Tags, t)
		}
	}
	for _, f := range d.Fields {
		e.Custom = append(e.Custom, vault.Field{Name: f.Key, Value: f.Value, Protected: f.Protected})
	}
	return e
}

// reparent rewrites a folder's path and everything filed under it.
func reparent(entries []vault.Entry, folders []vault.Folder, source, from, to, id, newParent string) ([]vault.Entry, []vault.Folder) {
	if from == to {
		return entries, folders
	}
	rewrite := func(p string) (string, bool) {
		switch {
		case p == from:
			return to, true
		case strings.HasPrefix(p, from+"/"):
			return to + strings.TrimPrefix(p, from), true
		}
		return p, false
	}
	for i := range folders {
		if folders[i].Source != source {
			continue
		}
		if p, ok := rewrite(folders[i].Path); ok {
			folders[i].Path = p
			if folders[i].ID == id {
				folders[i].Name = p[strings.LastIndex(p, "/")+1:]
				if newParent != "" {
					folders[i].ParentID = newParent
				}
			}
		}
	}
	for i := range entries {
		if entries[i].Source != source {
			continue
		}
		if p, ok := rewrite(entries[i].Path); ok {
			entries[i].Path = p
		}
	}
	return entries, folders
}
