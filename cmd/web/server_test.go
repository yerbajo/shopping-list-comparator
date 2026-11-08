package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yerbajo/shopping-list-comparator/internal/catalog"
	"github.com/yerbajo/shopping-list-comparator/internal/identity"
	"github.com/yerbajo/shopping-list-comparator/internal/resolve"
	"github.com/yerbajo/shopping-list-comparator/internal/store"
	"github.com/yerbajo/shopping-list-comparator/web"
)

type fakeSearch struct{ reply catalog.Reply }

func (f fakeSearch) Search(context.Context, string, int, []string) (catalog.Reply, error) {
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
	r := resolve.New(fakeSearch{reply: milkReply()}, st)
	r.Gap = 0
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r.Start(ctx)
	s := &server{o: options{store: st, resolver: r, assets: web.FS, engine: "http://engine.test"}}
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
