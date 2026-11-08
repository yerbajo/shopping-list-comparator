package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yerbajo/shopping-list-comparator/internal/identity"
)

func TestAddPersistsAndReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "list.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	it, err := s.Add("  leche   entera ")
	if err != nil {
		t.Fatal(err)
	}
	if it.Query != "leche entera" {
		t.Errorf("query: got %q", it.Query)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("list not written: %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("the temp file must not survive the rename")
	}

	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got := again.Snapshot()
	if len(got.Items) != 1 || got.Items[0].ID != it.ID {
		t.Fatalf("reload: %+v", got)
	}
	if got.Items[0].Resolving {
		t.Error("an interrupted resolution must not come back as running")
	}
}

func TestAddRejectsEmpty(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "list.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add("   "); err != ErrEmptyQuery {
		t.Errorf("got %v, want ErrEmptyQuery", err)
	}
	if n := len(s.Snapshot().Items); n != 0 {
		t.Errorf("a rejected item must not be stored: %d items", n)
	}
}

func TestUpdateAndRemove(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "list.json"))
	if err != nil {
		t.Fatal(err)
	}
	it, _ := s.Add("arroz")
	now := time.Now().UTC()
	ok, err := s.Update(it.ID, func(i *Item) {
		i.Resolving = false
		i.ResolvedAt = &now
		i.Anchor = &identity.Product{Store: "dia", ID: "151", Name: "Arroz extra Dia Arrozona 1 Kg"}
		i.Matches = []identity.Match{{Level: identity.LevelSame, Reason: "marca blanca de la cadena"}}
	})
	if err != nil || !ok {
		t.Fatalf("update: ok=%v err=%v", ok, err)
	}
	got, _ := s.Get(it.ID)
	if got.Resolving || got.Anchor == nil || len(got.Matches) != 1 {
		t.Errorf("update not applied: %+v", got)
	}
	if ok, err := s.Remove(it.ID); err != nil || !ok {
		t.Fatalf("remove: ok=%v err=%v", ok, err)
	}
	if ok, _ := s.Remove("nope"); ok {
		t.Error("removing an unknown id must report false")
	}
	if n := len(s.Snapshot().Items); n != 0 {
		t.Errorf("list not empty: %d", n)
	}
}

func TestOpenRefusesABrokenDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "list.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("a corrupt list must be reported, never silently replaced")
	}
}

func TestSelectedChainsPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "list.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Selected(); len(got) != 0 {
		t.Errorf("a new list has no selection: %v", got)
	}
	if err := s.SetSelected([]string{"dia", "mas"}); err != nil {
		t.Fatal(err)
	}
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got := again.Selected()
	if len(got) != 2 || got[0] != "dia" || got[1] != "mas" {
		t.Errorf("selection not persisted: %v", got)
	}
	if err := again.SetSelected(nil); err != nil {
		t.Fatal(err)
	}
	if len(again.Selected()) != 0 {
		t.Error("clearing the selection must persist too")
	}
}
