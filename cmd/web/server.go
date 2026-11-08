// Command web serves the shopping list and its equivalents: one page, a JSON API over the
// list, and a background resolver that talks to the catalogue engine.
package main

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"strings"

	"github.com/yerbajo/shopping-list-comparator/internal/catalog"
	"github.com/yerbajo/shopping-list-comparator/internal/identity"
	"github.com/yerbajo/shopping-list-comparator/internal/resolve"
	"github.com/yerbajo/shopping-list-comparator/internal/store"
)

// Labels are the chain names a shopper reads. The store code stays the key everywhere else.
var labels = map[string]string{
	"dia": "DIA", "mercadona": "Mercadona", "mas": "MAS", "aldi": "ALDI",
	"lidl": "Lidl", "carrefour": "Carrefour", "alcampo": "Alcampo",
}

// confirmed are the levels that may enter a basket total. `probable` is a proposal: counting
// its price would turn "this might be the same product" into money.
var confirmed = map[string]bool{
	identity.LevelExact: true, identity.LevelSame: true, identity.LevelPicked: true,
}

// retryable are the engine codes a later attempt may resolve.
var retryable = map[string]bool{
	"quota": true, "blocked": true, "timeout": true, "network": true,
}

type options struct {
	store    *store.Store
	resolver *resolve.Resolver
	assets   fs.FS
	engine   string
}

type server struct{ o options }

func (s *server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("POST /api/items", s.handleAdd)
	mux.HandleFunc("DELETE /api/items/{id}", s.handleDelete)
	mux.HandleFunc("POST /api/items/{id}/anchor", s.handleAnchor)
	mux.HandleFunc("POST /api/items/{id}/pick", s.handlePick)
	mux.HandleFunc("POST /api/items/{id}/retry", s.handleRetry)
	mux.HandleFunc("POST /api/retry", s.handleRetryAll)
	if s.o.assets != nil {
		files := http.FileServerFS(s.o.assets)
		// Only the embedded paths are served: a catch-all GET would shadow the 405 that
		// ServeMux answers for a wrong verb on /api/items.
		for _, path := range []string{"/{$}", "/styles.css", "/app.js"} {
			mux.Handle("GET "+path, files)
		}
	}
	return mux
}

// ---------- documents ----------

type storeRef struct {
	Store string `json:"store"`
	Label string `json:"label"`
}

type cellView struct {
	Store   string            `json:"store"`
	Level   string            `json:"level"`
	Product *identity.Product `json:"product,omitempty"`
	Reason  string            `json:"reason,omitempty"`
	Code    string            `json:"code,omitempty"`
	Alts    []identity.Match  `json:"alts,omitempty"`
}

type itemView struct {
	ID        string            `json:"id"`
	Query     string            `json:"query"`
	Anchor    *identity.Product `json:"anchor,omitempty"`
	Resolving bool              `json:"resolving"`
	Error     string            `json:"error,omitempty"`
	Cells     []cellView        `json:"cells"`
}

type cartLine struct {
	Name       string `json:"name"`
	PriceCents int64  `json:"priceCents"`
}

type cartView struct {
	Store      string     `json:"store"`
	Label      string     `json:"label"`
	Items      int        `json:"items"`
	Count      int        `json:"count"`
	TotalCents int64      `json:"totalCents"`
	Lines      []cartLine `json:"lines"`
	Missing    []string   `json:"missing"`
	Retry      bool       `json:"retry"`
}

type stateDoc struct {
	Reachable bool       `json:"reachable"`
	Engine    string     `json:"engine"`
	Stores    []storeRef `json:"stores"`
	Items     []itemView `json:"items"`
	Carts     []cartView `json:"carts"`
}

// state builds the whole page state from the list. A chain's answer, its absence and its
// failure are three different things and the UI needs all three.
func (s *server) state() stateDoc {
	snap := s.o.store.Snapshot()
	doc := stateDoc{
		Reachable: s.o.resolver.LastError() == "",
		Engine:    s.o.engine,
		Stores:    make([]storeRef, 0, len(catalog.Stores)),
		Items:     make([]itemView, 0, len(snap.Items)),
		Carts:     make([]cartView, 0, len(catalog.Stores)),
	}
	for _, chain := range catalog.Stores {
		doc.Stores = append(doc.Stores, storeRef{Store: chain, Label: labels[chain]})
	}

	for _, it := range snap.Items {
		doc.Items = append(doc.Items, s.itemView(it))
	}

	for _, chain := range catalog.Stores {
		cart := cartView{Store: chain, Label: labels[chain], Items: len(snap.Items),
			Lines: []cartLine{}, Missing: []string{}}
		for _, it := range snap.Items {
			m, ok := matchOf(it, chain)
			if ok && confirmed[m.Level] && m.Product.PriceCents != nil {
				cart.Count++
				cart.TotalCents += *m.Product.PriceCents
				cart.Lines = append(cart.Lines, cartLine{Name: m.Product.Name, PriceCents: *m.Product.PriceCents})
				continue
			}
			cart.Missing = append(cart.Missing, it.Query)
			if retryable[it.Status[chain]] {
				cart.Retry = true
			}
		}
		doc.Carts = append(doc.Carts, cart)
	}
	return doc
}

func (s *server) itemView(it store.Item) itemView {
	v := itemView{ID: it.ID, Query: it.Query, Anchor: it.Anchor, Resolving: it.Resolving, Error: it.Error}
	for _, chain := range catalog.Stores {
		c := cellView{Store: chain}
		code := it.Status[chain]
		m, has := matchOf(it, chain)
		switch {
		case has:
			product := m.Product
			c.Level, c.Product, c.Reason = m.Level, &product, m.Reason
			c.Alts = alternatives(it, chain, m.Product.ID)
		case it.Resolving:
			c.Level = "pending"
		case code != "":
			c.Level, c.Code = "unavailable", code
		default:
			c.Level = "no"
		}
		v.Cells = append(v.Cells, c)
	}
	return v
}

func matchOf(it store.Item, chain string) (identity.Match, bool) {
	for _, m := range it.Matches {
		if m.Product.Store == chain {
			return m, true
		}
	}
	return identity.Match{}, false
}

// alternatives are the other products of that chain, judged again so the shopper sees why
// each one is on the list. They are already on disk: offering them costs no search.
func alternatives(it store.Item, chain, chosen string) []identity.Match {
	if it.Anchor == nil {
		return nil
	}
	out := make([]identity.Match, 0, 4)
	for _, cand := range it.Candidates {
		if cand.Store != chain || cand.ID == chosen {
			continue
		}
		m := identity.Compare(it.Query, *it.Anchor, cand)
		if m.Level == identity.LevelNo {
			continue
		}
		out = append(out, m)
		if len(out) == 4 {
			break
		}
	}
	return out
}

// ---------- handlers ----------

func (s *server) handleState(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.state())
}

func (s *server) handleAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Query string `json:"query"`
	}
	if !decode(w, r, &body) {
		return
	}
	it, err := s.o.store.Add(body.Query)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.o.resolver.Enqueue(it.ID)
	writeJSON(w, http.StatusCreated, s.state())
}

func (s *server) handleDelete(w http.ResponseWriter, r *http.Request) {
	ok, err := s.o.store.Remove(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "ese producto no está en la lista")
		return
	}
	writeJSON(w, http.StatusOK, s.state())
}

// handleAnchor changes the base product of an item and recomputes the equivalents from the
// candidates already on disk: no chain is asked again, so changing your mind is free.
func (s *server) handleAnchor(w http.ResponseWriter, r *http.Request) {
	it, chosen, ok := s.lookup(w, r)
	if !ok {
		return
	}
	byStore := map[string][]identity.Product{}
	for _, c := range it.Candidates {
		byStore[c.Store] = append(byStore[c.Store], c)
	}
	matches := []identity.Match{}
	for _, chain := range catalog.Stores {
		hits := byStore[chain]
		if len(hits) == 0 {
			continue
		}
		best, _ := identity.Best(it.Query, chosen, hits)
		if best.Level == identity.LevelNo {
			continue
		}
		matches = append(matches, best)
	}
	if _, err := s.o.store.Update(it.ID, func(i *store.Item) {
		i.Anchor = &chosen
		i.Matches = matches
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.state())
}

// handlePick pins one chain's product to a candidate. It is the only claim in the app that a
// heuristic did not make, so it gets its own level and enters that chain's basket.
func (s *server) handlePick(w http.ResponseWriter, r *http.Request) {
	it, chosen, ok := s.lookup(w, r)
	if !ok {
		return
	}
	matches := make([]identity.Match, 0, len(it.Matches)+1)
	replaced := false
	for _, m := range it.Matches {
		if m.Product.Store == chosen.Store {
			matches = append(matches, identity.Match{
				Product: chosen, Level: identity.LevelPicked, Reason: "elegida por ti", Score: 100,
			})
			replaced = true
			continue
		}
		matches = append(matches, m)
	}
	if !replaced {
		matches = append(matches, identity.Match{
			Product: chosen, Level: identity.LevelPicked, Reason: "elegida por ti", Score: 100,
		})
	}
	if _, err := s.o.store.Update(it.ID, func(i *store.Item) { i.Matches = matches }); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.state())
}

func (s *server) handleRetry(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.o.store.Get(id); !ok {
		writeError(w, http.StatusNotFound, "ese producto no está en la lista")
		return
	}
	if _, err := s.o.store.Update(id, func(i *store.Item) { i.Resolving = true; i.Error = "" }); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.o.resolver.Enqueue(id)
	writeJSON(w, http.StatusOK, s.state())
}

func (s *server) handleRetryAll(w http.ResponseWriter, _ *http.Request) {
	for _, it := range s.o.store.Snapshot().Items {
		if it.Error != "" {
			_, _ = s.o.store.Update(it.ID, func(i *store.Item) { i.Resolving = true; i.Error = "" })
		}
	}
	s.o.resolver.Retry()
	writeJSON(w, http.StatusOK, s.state())
}

// lookup resolves the item and the candidate named in the body.
func (s *server) lookup(w http.ResponseWriter, r *http.Request) (store.Item, identity.Product, bool) {
	var body struct {
		Store string `json:"store"`
		ID    string `json:"id"`
	}
	if !decode(w, r, &body) {
		return store.Item{}, identity.Product{}, false
	}
	it, ok := s.o.store.Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "ese producto no está en la lista")
		return store.Item{}, identity.Product{}, false
	}
	for _, c := range it.Candidates {
		if c.Store == strings.TrimSpace(body.Store) && c.ID == strings.TrimSpace(body.ID) {
			return it, c, true
		}
	}
	writeError(w, http.StatusBadRequest, "ese producto no está entre las alternativas de la lista")
	return store.Item{}, identity.Product{}, false
}

// ---------- plumbing ----------

const maxBody = 1 << 20

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "falta el cuerpo de la petición")
			return false
		}
		writeError(w, http.StatusBadRequest, "cuerpo ilegible: "+err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
