package resolve

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/yerbajo/shopping-list-comparator/internal/catalog"
	"github.com/yerbajo/shopping-list-comparator/internal/identity"
	"github.com/yerbajo/shopping-list-comparator/internal/store"
)

func prod(store, name, brand, size string, cents int64) identity.Product {
	c := cents
	return identity.Product{Store: store, ID: store + ":" + name, Name: name, Brand: brand, Size: size, PriceCents: &c}
}

type fakeSearch struct {
	mu    sync.Mutex
	reply catalog.Reply
	err   error
	calls int
}

func (f *fakeSearch) Search(_ context.Context, _ string, _ int, _ []string) (catalog.Reply, error) {
	f.mu.Lock()
	f.calls++
	err := f.err
	f.mu.Unlock()
	if err != nil {
		return catalog.Reply{}, err
	}
	return f.reply, nil
}

func (f *fakeSearch) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func milkReply() catalog.Reply {
	return catalog.Reply{Query: "leche entera", Results: []catalog.ChainResult{
		{Store: "dia", Hits: []identity.Product{
			prod("dia", "Leche entera Dia Láctea 1 L", "Dia Láctea", "", 96),
			prod("dia", "Leche entera Dia Láctea 6 x 1 L", "Dia Láctea", "", 576),
		}},
		{Store: "mercadona", Hits: []identity.Product{
			prod("mercadona", "Leche entera Hacendado", "Hacendado", "1 l", 96),
			prod("mercadona", "Leche entera sin lactosa Hacendado", "Hacendado", "1 l", 94),
		}},
		{Store: "mas", Hits: []identity.Product{
			prod("mas", "Leche entera ACORES brick 1l", "", "", 95),
		}},
		{Store: "aldi", Hits: []identity.Product{
			prod("aldi", "Leche entera", "MILSANI®", "", 96),
		}},
		{Store: "lidl", Hits: nil},
		{Store: "carrefour", Err: &catalog.Error{Code: "quota", Retryable: true, Message: "3 req/min"}},
		{Store: "alcampo", Err: &catalog.Error{Code: "quota", Status: 202, Retryable: true, Message: "WAF"}},
	}}
}

func newResolver(t *testing.T, f *fakeSearch) (*Resolver, *store.Store, string) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "list.json"))
	if err != nil {
		t.Fatal(err)
	}
	it, err := st.Add("leche entera")
	if err != nil {
		t.Fatal(err)
	}
	r := New(f, st)
	r.Gap = 0
	return r, st, it.ID
}

func TestResolvePicksAnchorAndOneEquivalentPerChain(t *testing.T) {
	f := &fakeSearch{reply: milkReply()}
	r, st, id := newResolver(t, f)
	if err := r.Resolve(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	it, _ := st.Get(id)
	if it.Resolving {
		t.Error("the item must stop resolving")
	}
	if it.ResolvedAt == nil {
		t.Error("resolvedAt must be stamped")
	}
	if it.Anchor == nil || it.Anchor.Store != "dia" || it.Anchor.Name != "Leche entera Dia Láctea 1 L" {
		t.Errorf("anchor: %+v", it.Anchor)
	}
	got := map[string]string{}
	for _, m := range it.Matches {
		got[m.Product.Store] = m.Level
	}
	want := map[string]string{
		"dia":       identity.LevelSame,
		"mercadona": identity.LevelSame,
		"mas":       identity.LevelSame,
		"aldi":      identity.LevelMaybe,
	}
	if len(got) != len(want) {
		t.Fatalf("matches: %v", got)
	}
	for chain, level := range want {
		if got[chain] != level {
			t.Errorf("%s: got %q, want %q", chain, got[chain], level)
		}
	}
	if it.Status["alcampo"] != "quota" || it.Status["carrefour"] != "quota" {
		t.Errorf("chain statuses lost: %v", it.Status)
	}
	if _, ok := it.Status["lidl"]; !ok {
		t.Error("a chain that answered with nothing must still report its state")
	}
	if len(it.Candidates) == 0 {
		t.Error("the alternatives must be kept so a shopper can overrule the machine")
	}
}

func TestResolveKeepsAPinnedAnchor(t *testing.T) {
	f := &fakeSearch{reply: milkReply()}
	r, st, id := newResolver(t, f)
	pinned := prod("dia", "Leche entera Dia Láctea 6 x 1 L", "Dia Láctea", "", 576)
	if _, err := st.Update(id, func(i *store.Item) { i.Anchor = &pinned }); err != nil {
		t.Fatal(err)
	}
	if err := r.Resolve(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	it, _ := st.Get(id)
	if it.Anchor.Name != pinned.Name {
		t.Errorf("anchor overwritten: %+v", it.Anchor)
	}
	for _, m := range it.Matches {
		if m.Level == identity.LevelSame && m.Product.Size != "" {
			if m.Product.Size != "1 L" && m.Product.Size != "1 l" {
				t.Errorf("equivalent must share the pinned pack size: %+v", m)
			}
		}
	}
}

func TestResolveReportsAnEmptyCatalogue(t *testing.T) {
	f := &fakeSearch{reply: catalog.Reply{Results: []catalog.ChainResult{
		{Store: "dia", Hits: nil},
		{Store: "mercadona", Err: &catalog.Error{Code: "timeout"}},
	}}}
	r, st, id := newResolver(t, f)
	if err := r.Resolve(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	it, _ := st.Get(id)
	if it.Error == "" {
		t.Error("an item nobody sells must say so")
	}
	if it.Resolving || it.ResolvedAt == nil {
		t.Errorf("state: %+v", it)
	}
	if it.Anchor != nil {
		t.Errorf("no anchor must be invented: %+v", it.Anchor)
	}
}

func TestResolveReportsAnUnreachableEngine(t *testing.T) {
	f := &fakeSearch{err: errors.New("motor de catálogo no disponible")}
	r, st, id := newResolver(t, f)
	if err := r.Resolve(context.Background(), id); err == nil {
		t.Fatal("the error must reach the caller")
	}
	it, _ := st.Get(id)
	if it.Error == "" || it.Resolving {
		t.Errorf("state after a transport failure: %+v", it)
	}
}

func TestStartDrainsTheQueueOnce(t *testing.T) {
	f := &fakeSearch{reply: milkReply()}
	r, st, id := newResolver(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Start(ctx)
	r.Enqueue(id)
	r.Enqueue(id) // the second press must not spend the quota twice

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if it, ok := st.Get(id); ok && it.ResolvedAt != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if it, _ := st.Get(id); it.ResolvedAt == nil {
		t.Fatal("the item was never resolved")
	}
	if n := f.count(); n != 1 {
		t.Errorf("engine called %d times, want 1", n)
	}
}

func TestRetryCountsTheChainsThatCanAnswerLater(t *testing.T) {
	f := &fakeSearch{reply: milkReply()}
	r, _, id := newResolver(t, f)
	if err := r.Resolve(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if n := r.Retry(); n != 1 {
		t.Errorf("retry: got %d items, want the one with a quota failure", n)
	}
	if !r.Pending(id) {
		t.Error("retry must enqueue the item")
	}
}
