package main

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// fakeDoc is a stateful stand-in for a Feishu document: creates honour the
// request's "index" the way the API does (-1 appends), deletes remove the block,
// and GetDocumentBlocks reports the current tree. Blocks are named by their text.
type fakeDoc struct {
	*mockClient
	order    []string            // page children, by block id
	text     map[string]string   // block id -> text content
	children map[string][]string // block id -> child block ids
	failDel  map[string]bool     // deletes of these ids fail silently
	seq      int
}

func newFakeDoc(texts ...string) *fakeDoc {
	d := &fakeDoc{mockClient: &mockClient{}, text: map[string]string{}, children: map[string][]string{}, failDel: map[string]bool{}}
	for i, t := range texts {
		id := fmt.Sprintf("old%d", i)
		d.order = append(d.order, id)
		d.text[id] = t
	}
	return d
}

func (d *fakeDoc) GetDocumentBlocks(ctx context.Context, input string) (any, error) {
	order := make([]any, len(d.order))
	for i, id := range d.order {
		order[i] = id
	}
	items := []any{map[string]any{"block_id": "doc123", "block_type": float64(btPage), "children": order}}
	for id, t := range d.text {
		kids := make([]any, 0, len(d.children[id]))
		for _, k := range d.children[id] {
			kids = append(kids, k)
		}
		items = append(items, map[string]any{
			"block_id":   id,
			"block_type": float64(btText),
			"children":   kids,
			"text":       map[string]any{"elements": []any{map[string]any{"text_run": map[string]any{"content": t}}}},
		})
	}
	return map[string]any{"items": items}, nil
}

func (d *fakeDoc) CreateDocumentBlocks(ctx context.Context, docID, parent string, body any) (any, error) {
	b := body.(map[string]any)
	kids := b["children"].([]map[string]any)
	index := b["index"].(int)
	var ids []string
	var created []any
	for _, k := range kids {
		d.seq++
		id := fmt.Sprintf("new%d", d.seq)
		d.text[id] = firstTextRun(k)
		ids = append(ids, id)
		created = append(created, map[string]any{"block_id": id})
	}
	if parent == "" || parent == docID {
		d.order = insertAt(d.order, index, ids)
	} else {
		d.children[parent] = insertAt(d.children[parent], index, ids)
	}
	return map[string]any{"children": created}, nil
}

func (d *fakeDoc) DeleteDocumentBlock(ctx context.Context, docID, blockID string) (any, error) {
	if d.failDel[blockID] {
		return nil, fmt.Errorf("delete %s failed", blockID)
	}
	for i, id := range d.order {
		if id == blockID {
			d.order = append(d.order[:i:i], d.order[i+1:]...)
			break
		}
	}
	return nil, nil
}

func insertAt(list []string, index int, ids []string) []string {
	if index < 0 || index > len(list) {
		index = len(list)
	}
	out := append([]string{}, list[:index]...)
	out = append(out, ids...)
	return append(out, list[index:]...)
}

func firstTextRun(v any) string {
	switch t := v.(type) {
	case map[string]any:
		if tr, ok := t["text_run"].(map[string]any); ok {
			s, _ := tr["content"].(string)
			return s
		}
		for _, x := range t {
			if s := firstTextRun(x); s != "" {
				return s
			}
		}
	case []any:
		for _, x := range t {
			if s := firstTextRun(x); s != "" {
				return s
			}
		}
	case []map[string]any:
		for _, x := range t {
			if s := firstTextRun(x); s != "" {
				return s
			}
		}
	}
	return ""
}

// pageTexts is the document as the reader sees it: the page children in order.
func (d *fakeDoc) pageTexts() []string {
	out := make([]string, len(d.order))
	for i, id := range d.order {
		out[i] = d.text[id]
	}
	return out
}

func para(t string) map[string]any {
	return map[string]any{"block_type": btText, "text": map[string]any{"elements": []any{map[string]any{"text_run": map[string]any{"content": t}}}}}
}

func keep(id, t string) mdBlockEntry {
	return mdBlockEntry{BlockID: id, BlockType: btText, Body: para(t), Comparable: t}
}

func add(t string) mdBlockEntry {
	return mdBlockEntry{BlockType: btText, Body: para(t), Comparable: t}
}

func TestDoDiffUpdatePlacesNewBlocksWhereTheyAreWritten(t *testing.T) {
	d := newFakeDoc("A", "B", "C")
	entries := []mdBlockEntry{
		add("top"),
		keep("old0", "A"),
		add("after A 1"),
		add("after A 2"),
		keep("old1", "B"),
		// C was removed from the markdown
		add("end"),
	}
	if err := doDiffUpdate(context.Background(), d, "doc123", entries); err != nil {
		t.Fatal(err)
	}
	// "end" follows B, the last kept block above it, once C is gone.
	want := []string{"top", "A", "after A 1", "after A 2", "B", "end"}
	if got := d.pageTexts(); !reflect.DeepEqual(got, want) {
		t.Fatalf("page = %q, want %q", got, want)
	}
}

func TestDoDiffUpdateLongRunKeepsOrderAcrossBatches(t *testing.T) {
	d := newFakeDoc("A", "B")
	entries := []mdBlockEntry{keep("old0", "A")}
	var want []string
	want = append(want, "A")
	for i := range 23 { // three create batches of at most 10
		s := fmt.Sprintf("n%02d", i)
		entries = append(entries, add(s))
		want = append(want, s)
	}
	entries = append(entries, keep("old1", "B"))
	want = append(want, "B")
	if err := doDiffUpdate(context.Background(), d, "doc123", entries); err != nil {
		t.Fatal(err)
	}
	if got := d.pageTexts(); !reflect.DeepEqual(got, want) {
		t.Fatalf("page = %q, want %q", got, want)
	}
}

func TestDoDiffUpdatePlacesAgainstThePageAfterAFailedDelete(t *testing.T) {
	// Deletes are best effort. If one fails the block stays, and new blocks
	// must still land after their anchor rather than one slot early.
	d := newFakeDoc("A", "gone", "B")
	d.failDel["old1"] = true
	entries := []mdBlockEntry{keep("old0", "A"), keep("old2", "B"), add("after B")}
	if err := doDiffUpdate(context.Background(), d, "doc123", entries); err != nil {
		t.Fatal(err)
	}
	want := []string{"A", "gone", "B", "after B"}
	if got := d.pageTexts(); !reflect.DeepEqual(got, want) {
		t.Fatalf("page = %q, want %q", got, want)
	}
}

func TestDoDiffUpdateCreatesChildrenOfNewEntries(t *testing.T) {
	d := newFakeDoc("A", "B")
	parent := add("parent")
	parent.Children = []mdBlockEntry{add("child 1"), add("child 2")}
	entries := []mdBlockEntry{keep("old0", "A"), parent, keep("old1", "B")}
	if err := doDiffUpdate(context.Background(), d, "doc123", entries); err != nil {
		t.Fatal(err)
	}
	if got, want := d.pageTexts(), []string{"A", "parent", "B"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("page = %q, want %q", got, want)
	}
	pid := d.order[1]
	var kids []string
	for _, id := range d.children[pid] {
		kids = append(kids, d.text[id])
	}
	if want := []string{"child 1", "child 2"}; strings.Join(kids, "|") != strings.Join(want, "|") {
		t.Fatalf("children of parent = %q, want %q", kids, want)
	}
}
