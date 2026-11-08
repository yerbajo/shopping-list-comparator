package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yerbajo/shopping-list-comparator/internal/catalog"
	"github.com/yerbajo/shopping-list-comparator/internal/identity"
	"github.com/yerbajo/shopping-list-comparator/internal/resolve"
	"github.com/yerbajo/shopping-list-comparator/internal/store"
	"github.com/yerbajo/shopping-list-comparator/web"
)

type fakeSearch struct {
	mu     sync.Mutex
	reply  catalog.Reply
	calls  int
	stores []string
}

func (f *fakeSearch) Search(_ context.Context, _ string, _ int, stores []string) (catalog.Reply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.stores = append([]string(nil), stores...)
	return f.reply, nil
}

func prod(storeName, name, brand, size string, cents int64) identity.Product {
	c := cents
	return identity.Product{Store: storeName, ID: storeName + ":" + name, Name: name, Brand: brand, Size: size, PriceCents: &c}
}

func milkReply() catalog.Reply {
	return catalog.Reply{Query: "leche entera", Results: []catalog.ChainResult{
		{Store: "dia", Hits: []identity.Product{
			prod("dia", "Leche entera Dia Láctea 1 L", "Dia Láctea", "", 96),
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

func newTest(t *testing.T) (*httptest.Server, *store.Store, *resolve.Resolver) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "list.json"))
	if err != nil {
		t.Fatal(err)
	}
	search := &fakeSearch{reply: milkReply()}
	r := resolve.New(search, st)
	r.Gap = 0
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r.Start(ctx)
	s := &server{o: options{store: st, resolver: r, searcher: search, assets: web.FS, engine: "http://engine.test"}}
	ts := httptest.NewServer(s.handler())
	t.Cleanup(ts.Close)
	return ts, st, r
}

func call(t *testing.T, ts *httptest.Server, method, path string, body any) (*http.Response, stateDoc, string) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, ts.URL+path, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var payload json.RawMessage
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	var doc stateDoc
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		if err := json.Unmarshal(payload, &doc); err != nil {
			t.Fatalf("%s %s: %v (%s)", method, path, err, payload)
		}
	}
	return resp, doc, string(payload)
}

func waitResolved(t *testing.T, ts *httptest.Server) stateDoc {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_, doc, _ := call(t, ts, http.MethodGet, "/api/state", nil)
		if len(doc.Items) == 1 && !doc.Items[0].Resolving {
			return doc
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the item was never resolved")
	return stateDoc{}
}

func TestAddResolveAndBaskets(t *testing.T) {
	ts, _, _ := newTest(t)

	resp, doc, _ := call(t, ts, http.MethodPost, "/api/items", map[string]string{"query": "leche entera"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add: %s", resp.Status)
	}
	if len(doc.Items) != 1 {
		t.Fatalf("add must return the list with one item, got %d", len(doc.Items))
	}
	if len(doc.Items[0].Cells) != 7 {
		t.Fatalf("every item needs one cell per chain, got %d", len(doc.Items[0].Cells))
	}

	doc = waitResolved(t, ts)
	item := doc.Items[0]
	if item.Anchor == nil || item.Anchor.Store != "dia" {
		t.Fatalf("anchor: %+v", item.Anchor)
	}
	cells := map[string]cellView{}
	for _, c := range item.Cells {
		cells[c.Store] = c
	}
	for _, want := range []struct{ store, level string }{
		{"dia", identity.LevelSame}, {"mercadona", identity.LevelSame},
		{"mas", identity.LevelSame}, {"aldi", identity.LevelMaybe},
		{"lidl", "no"}, {"carrefour", "unavailable"}, {"alcampo", "unavailable"},
	} {
		if got := cells[want.store].Level; got != want.level {
			t.Errorf("%s: level %q, want %q", want.store, got, want.level)
		}
	}
	if cells["alcampo"].Code != "quota" {
		t.Errorf("alcampo must carry the engine code, got %q", cells["alcampo"].Code)
	}

	carts := map[string]cartView{}
	for _, c := range doc.Carts {
		carts[c.Store] = c
	}
	if carts["dia"].TotalCents != 96 || carts["dia"].Count != 1 {
		t.Errorf("dia basket: %+v", carts["dia"])
	}
	if carts["mas"].TotalCents != 95 {
		t.Errorf("mas basket: %+v", carts["mas"])
	}
	if carts["aldi"].Count != 0 {
		t.Errorf("a probable product must not enter a total: %+v", carts["aldi"])
	}
	if len(carts["aldi"].Missing) != 1 || carts["aldi"].Missing[0] != "leche entera" {
		t.Errorf("aldi missing: %+v", carts["aldi"].Missing)
	}
	if !carts["alcampo"].Retry || !carts["carrefour"].Retry {
		t.Error("a quota failure must offer a retry")
	}
	if carts["lidl"].Retry {
		t.Error("a chain that answered with nothing must not offer a retry")
	}
	if !doc.Reachable {
		t.Error("the engine answered")
	}
}

func TestPickEntersTheBasketWithoutTouchingTheEngine(t *testing.T) {
	ts, _, _ := newTest(t)
	call(t, ts, http.MethodPost, "/api/items", map[string]string{"query": "leche entera"})
	doc := waitResolved(t, ts)
	id := doc.Items[0].ID

	resp, doc, payload := call(t, ts, http.MethodPost, "/api/items/"+id+"/pick",
		map[string]string{"store": "aldi", "id": "aldi:Leche entera"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pick: %s %s", resp.Status, payload)
	}
	cells := map[string]cellView{}
	for _, c := range doc.Items[0].Cells {
		cells[c.Store] = c
	}
	if cells["aldi"].Level != identity.LevelPicked {
		t.Errorf("aldi level: %q", cells["aldi"].Level)
	}
	for _, c := range doc.Carts {
		if c.Store == "aldi" && (c.Count != 1 || c.TotalCents != 96) {
			t.Errorf("a picked product must count: %+v", c)
		}
	}
}

func TestAnchorChangeRecomputesFromDisk(t *testing.T) {
	ts, _, _ := newTest(t)
	call(t, ts, http.MethodPost, "/api/items", map[string]string{"query": "leche entera"})
	doc := waitResolved(t, ts)
	id := doc.Items[0].ID

	_, doc, payload := call(t, ts, http.MethodPost, "/api/items/"+id+"/anchor",
		map[string]string{"store": "mercadona", "id": "mercadona:Leche entera sin lactosa Hacendado"})
	if len(doc.Items) != 1 || doc.Items[0].Anchor == nil || doc.Items[0].Anchor.ID != "mercadona:Leche entera sin lactosa Hacendado" {
		t.Fatalf("anchor not changed: %s", payload)
	}
	cells := map[string]cellView{}
	for _, c := range doc.Items[0].Cells {
		cells[c.Store] = c
	}
	if cells["dia"].Level != identity.LevelMaybe {
		t.Errorf("plain milk against a lactose-free base must be probable, got %q", cells["dia"].Level)
	}
}

func TestRejectsUnknownCandidatesAndEmptyQueries(t *testing.T) {
	ts, _, _ := newTest(t)
	if resp, _, _ := call(t, ts, http.MethodPost, "/api/items", map[string]string{"query": "   "}); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("empty query: %s", resp.Status)
	}
	_, doc, _ := call(t, ts, http.MethodPost, "/api/items", map[string]string{"query": "leche entera"})
	doc = waitResolved(t, ts)
	id := doc.Items[0].ID
	resp, _, _ := call(t, ts, http.MethodPost, "/api/items/"+id+"/pick",
		map[string]string{"store": "aldi", "id": "not-a-candidate"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown candidate: %s", resp.Status)
	}
	if resp, _, _ := call(t, ts, http.MethodDelete, "/api/items/nope", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown item: %s", resp.Status)
	}
}

func TestDeleteEmptiesTheList(t *testing.T) {
	ts, st, _ := newTest(t)
	_, doc, _ := call(t, ts, http.MethodPost, "/api/items", map[string]string{"query": "leche entera"})
	doc = waitResolved(t, ts)
	resp, doc, _ := call(t, ts, http.MethodDelete, "/api/items/"+doc.Items[0].ID, nil)
	if resp.StatusCode != http.StatusOK || len(doc.Items) != 0 {
		t.Fatalf("delete: %s items=%d", resp.Status, len(doc.Items))
	}
	if n := len(st.Snapshot().Items); n != 0 {
		t.Errorf("store still holds %d items", n)
	}
	for _, c := range doc.Carts {
		if c.Count != 0 || len(c.Missing) != 0 {
			t.Errorf("an empty list must have empty baskets: %+v", c)
		}
	}
}

func TestServesTheInterfaceWithoutLeakingAPI(t *testing.T) {
	ts, _, _ := newTest(t)
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /: %s", resp.Status)
	}
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(resp.Body)
	if !strings.Contains(buf.String(), "La lista") {
		t.Error("the page does not carry the interface")
	}
	if resp, _ := http.Get(ts.URL + "/api/items"); resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("a wrong verb must be 405, got %s", resp.Status)
	}
}

func TestSearchOffersProductsPerChainAndCaches(t *testing.T) {
	ts, st, _ := newTest(t)
	resp, payload := raw(t, ts, http.MethodPost, "/api/search", map[string]string{"query": "leche entera"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search: %s %s", resp.Status, payload)
	}
	var doc pickerDoc
	if err := json.Unmarshal([]byte(payload), &doc); err != nil {
		t.Fatalf("decode: %v (%s)", err, payload)
	}
	byStore := map[string]pickerChain{}
	for _, c := range doc.Results {
		byStore[c.Store] = c
	}
	if len(byStore["dia"].Products) == 0 || byStore["dia"].Products[0].Name == "" {
		t.Errorf("dia offers no product: %+v", byStore["dia"])
	}
	if byStore["carrefour"].Code != "quota" {
		t.Errorf("a chain that could not answer must report its code: %+v", byStore["carrefour"])
	}
	for _, c := range doc.Results {
		for _, p := range c.Products {
			if p.PriceCents == nil {
				t.Errorf("%s offers a product with no price: %+v", c.Store, p)
			}
		}
	}
	// A search must never write the list.
	if n := len(st.Snapshot().Items); n != 0 {
		t.Errorf("the picker wrote %d items into the list", n)
	}
}

// raw is call without decoding into stateDoc: the picker answers a different document.
func raw(t *testing.T, ts *httptest.Server, method, path string, body any) (*http.Response, string) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, ts.URL+path, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out := new(bytes.Buffer)
	_, _ = out.ReadFrom(resp.Body)
	return resp, out.String()
}

func TestSearchDropsAnotherDomain(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "list.json"))
	if err != nil {
		t.Fatal(err)
	}
	search := &fakeSearch{reply: catalog.Reply{Results: []catalog.ChainResult{
		{Store: "aldi", Hits: []identity.Product{
			prod("aldi", "Huevos de chocolate rellenos de leche", "CHOCEUR®", "", 299),
			prod("aldi", "Huevos frescos clase L", "MILSANI®", "12 ud", 285),
		}},
	}}}
	r := resolve.New(search, st)
	srv := &server{o: options{store: st, resolver: r, searcher: search, engine: "http://engine.test"}}
	ts := httptest.NewServer(srv.handler())
	t.Cleanup(ts.Close)
	resp, payload := raw(t, ts, http.MethodPost, "/api/search", map[string]string{"query": "huevos"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search: %s %s", resp.Status, payload)
	}
	var doc pickerDoc
	if err := json.Unmarshal([]byte(payload), &doc); err != nil {
		t.Fatal(err)
	}
	for _, c := range doc.Results {
		for _, p := range c.Products {
			if strings.Contains(p.Name, "chocolate") {
				t.Errorf("the picker offered chocolate eggs for \"huevos\": %+v", p)
			}
		}
	}
}

func TestAddWithAChosenProductUsesItAsBase(t *testing.T) {
	ts, _, _ := newTest(t)
	anchor := prod("mas", "Leche entera IFA ELIGES brick 1l", "", "1 l", 96)
	resp, doc, payload := call(t, ts, http.MethodPost, "/api/items", map[string]any{
		"query": "leche entera", "anchor": anchor,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add with anchor: %s %s", resp.Status, payload)
	}
	if doc.Items[0].Anchor == nil || doc.Items[0].Anchor.Store != "mas" {
		t.Fatalf("anchor ignored: %+v", doc.Items[0].Anchor)
	}
	if doc.Items[0].Section != identity.SectionFresh {
		t.Errorf("milk must be filed as fresh: %q", doc.Items[0].Section)
	}
}

func TestAddRejectsAnAnchorWithoutPrice(t *testing.T) {
	ts, _, _ := newTest(t)
	bad := prod("dia", "Leche entera Dia Láctea 1 L", "Dia Láctea", "", 96)
	bad.PriceCents = nil
	resp, _, _ := call(t, ts, http.MethodPost, "/api/items", map[string]any{"query": "leche", "anchor": bad})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("got %s, want 400", resp.Status)
	}
}

func TestPatchQuantityAndSection(t *testing.T) {
	ts, _, _ := newTest(t)
	_, doc, _ := call(t, ts, http.MethodPost, "/api/items", map[string]string{"query": "leche entera"})
	doc = waitResolved(t, ts)
	id := doc.Items[0].ID

	_, doc, payload := call(t, ts, http.MethodPatch, "/api/items/"+id, map[string]any{"quantity": 3})
	if doc.Items[0].Quantity != 3 {
		t.Fatalf("quantity not stored: %s", payload)
	}
	carts := map[string]cartView{}
	for _, c := range doc.Carts {
		carts[c.Store] = c
	}
	if got := carts["dia"].TotalCents; got != 3*96 {
		t.Errorf("the basket must multiply the quantity: got %d, want %d", got, 3*96)
	}
	if got := carts["dia"].Lines[0].Quantity; got != 3 {
		t.Errorf("line quantity: got %d", got)
	}

	_, doc, payload = call(t, ts, http.MethodPatch, "/api/items/"+id, map[string]any{"section": identity.SectionPantry})
	if doc.Items[0].Section != identity.SectionPantry {
		t.Fatalf("section not stored: %s", payload)
	}
	for _, bad := range []map[string]any{{"quantity": 0}, {"quantity": 100}, {"section": "garaje"}} {
		if resp, _, _ := call(t, ts, http.MethodPatch, "/api/items/"+id, bad); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%v: got %s, want 400", bad, resp.Status)
		}
	}
}

func TestStoreSelectionDrivesSearchCellsAndBaskets(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "list.json"))
	if err != nil {
		t.Fatal(err)
	}
	search := &fakeSearch{reply: milkReply()}
	r := resolve.New(search, st)
	r.Gap = 0
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r.Start(ctx)
	srv := &server{o: options{store: st, resolver: r, searcher: search, assets: web.FS, engine: "http://engine.test"}}
	ts := httptest.NewServer(srv.handler())
	t.Cleanup(ts.Close)

	// By default every chain is in play.
	_, doc, _ := call(t, ts, http.MethodPost, "/api/items", map[string]string{"query": "leche entera"})
	if len(doc.Stores) != 7 || len(doc.Carts) != 7 {
		t.Fatalf("default: %d stores, %d baskets", len(doc.Stores), len(doc.Carts))
	}

	// Leaving two of them changes the cells, the baskets and what the engine is asked.
	_, doc, payload := call(t, ts, http.MethodPost, "/api/stores", map[string]any{"chains": []string{"dia", "mas"}})
	if len(doc.Carts) != 2 || doc.Carts[0].Store != "dia" || doc.Carts[1].Store != "mas" {
		t.Fatalf("baskets: %+v (%s)", doc.Carts, payload)
	}
	for _, ref := range doc.Stores {
		want := ref.Store == "dia" || ref.Store == "mas"
		if ref.Selected != want {
			t.Errorf("%s: selected=%v, want %v", ref.Store, ref.Selected, want)
		}
	}
	doc = waitResolved(t, ts)
	for _, item := range doc.Items {
		if len(item.Cells) != 2 {
			t.Errorf("%s: %d cells, want one per chosen chain", item.Query, len(item.Cells))
		}
	}

	// A product added now is asked only to the chosen chains: that is the quota saving.
	search.mu.Lock()
	search.stores = nil
	search.mu.Unlock()
	call(t, ts, http.MethodPost, "/api/items", map[string]string{"query": "pan de molde"})
	waitAll(t, ts)
	search.mu.Lock()
	asked := append([]string(nil), search.stores...)
	search.mu.Unlock()
	if len(asked) != 2 || asked[0] != "dia" || asked[1] != "mas" {
		t.Errorf("the engine was asked for %v, want the two chosen chains", asked)
	}

	// Re-enabling a chain asks only for what was never answered.
	_, doc, _ = call(t, ts, http.MethodPost, "/api/stores", map[string]any{"chains": []string{"dia", "mas", "aldi"}})
	if len(doc.Carts) != 3 {
		t.Fatalf("baskets after re-enabling: %+v", doc.Carts)
	}

	// The selection survives a restart.
	if got := st.Selected(); len(got) != 3 {
		t.Errorf("stored selection: %v", got)
	}
}

// waitAll waits until no item is resolving.
func waitAll(t *testing.T, ts *httptest.Server) stateDoc {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_, doc, _ := call(t, ts, http.MethodGet, "/api/state", nil)
		busy := false
		for _, it := range doc.Items {
			if it.Resolving {
				busy = true
			}
		}
		if !busy && len(doc.Items) > 0 {
			return doc
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("items never stopped resolving")
	return stateDoc{}
}

func TestStoreSelectionRejectsEmptyAndUnknown(t *testing.T) {
	ts, _, _ := newTest(t)
	for _, body := range []map[string]any{
		{"chains": []string{}},
		{"chains": []string{"dia", "eroski"}},
	} {
		if resp, _, _ := call(t, ts, http.MethodPost, "/api/stores", body); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%v: got %s, want 400", body, resp.Status)
		}
	}
}
