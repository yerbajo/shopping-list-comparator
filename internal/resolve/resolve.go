// Package resolve turns a list item into one equivalent per chain.
//
// It runs in the background, one item at a time, with a pause between searches: a list of
// twenty items would otherwise fire twenty fan-outs of seven chains and burn the quotas the
// measured pass already ran into (Carrefour tolerates three requests a minute, Alcampo
// answers an AWS WAF challenge). The queue is the product decision that makes a list
// possible at all; without it every add is a denial of service on your own account.
package resolve

import (
	"context"
	"sync"
	"time"

	"github.com/yerbajo/shopping-list-comparator/internal/catalog"
	"github.com/yerbajo/shopping-list-comparator/internal/identity"
	"github.com/yerbajo/shopping-list-comparator/internal/store"
)

// Defaults for a list that is read by one person.
const (
	DefaultLimit = 20              // products asked of each chain per item
	DefaultKeep  = 5               // alternatives kept per chain for the manual pick
	DefaultGap   = 4 * time.Second // pause between searches, shared by every chain
)

// Resolver resolves items in the background.
type Resolver struct {
	Search catalog.Searcher
	Store  *store.Store
	Limit  int
	Keep   int
	Gap    time.Duration

	queue   chan string
	mu      sync.Mutex
	pending map[string]bool
	started bool
	lastErr string
}

// New builds a resolver.
func New(search catalog.Searcher, st *store.Store) *Resolver {
	return &Resolver{
		Search:  search,
		Store:   st,
		Limit:   DefaultLimit,
		Keep:    DefaultKeep,
		Gap:     DefaultGap,
		queue:   make(chan string, 256),
		pending: map[string]bool{},
	}
}

// Enqueue asks for one item to be resolved. A second call for an item already waiting is
// ignored: the shopper pressing the button twice must not spend the quota twice.
func (r *Resolver) Enqueue(id string) {
	r.mu.Lock()
	if r.pending[id] {
		r.mu.Unlock()
		return
	}
	if _, ok := r.Store.Get(id); !ok {
		r.mu.Unlock()
		return
	}
	r.pending[id] = true
	r.mu.Unlock()
	select {
	case r.queue <- id:
	default: // the queue is bounded; a full one drops the work, and the item keeps its state
		r.mu.Lock()
		delete(r.pending, id)
		r.mu.Unlock()
	}
}

// LastError is the last transport failure against the catalogue engine, empty when the last
// search reached it. The interface shows "motor sin conexión" from this and nothing else.
func (r *Resolver) LastError() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastErr
}

func (r *Resolver) setLastError(msg string) {
	r.mu.Lock()
	r.lastErr = msg
	r.mu.Unlock()
}

// Pending reports whether an item waits in the queue.
func (r *Resolver) Pending(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pending[id]
}

// Start runs the worker until ctx is done. It is idempotent.
func (r *Resolver) Start(ctx context.Context) {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return
	}
	r.started = true
	r.mu.Unlock()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case id := <-r.queue:
				searchCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
				_ = r.Resolve(searchCtx, id)
				cancel()
				r.mu.Lock()
				delete(r.pending, id)
				r.mu.Unlock()
				select {
				case <-time.After(r.Gap):
				case <-ctx.Done():
					return
				}
			}
		}
	}()
}

// Resolve searches the chains for one item and stores the result. It is synchronous so a
// test, and the retry button, can wait for it.
func (r *Resolver) Resolve(ctx context.Context, id string) error {
	item, ok := r.Store.Get(id)
	if !ok {
		return nil
	}
	_, _ = r.Store.Update(id, func(i *store.Item) { i.Resolving = true; i.Error = "" })

	reply, err := r.Search.Search(ctx, item.Query, r.limit(), nil)
	r.setLastError(errText(err))
	if err != nil {
		_, _ = r.Store.Update(id, func(i *store.Item) {
			i.Resolving = false
			i.Error = err.Error()
		})
		return err
	}

	byStore := map[string][]identity.Product{}
	status := map[string]string{}
	all := make([]identity.Product, 0, len(catalog.Stores)*4)
	for _, res := range reply.Results {
		if res.Err != nil {
			status[res.Store] = res.Err.Code
			continue
		}
		status[res.Store] = ""
		for _, h := range res.Hits {
			if h.PriceCents == nil {
				continue // a product with no price cannot join a basket
			}
			byStore[res.Store] = append(byStore[res.Store], h)
			all = append(all, h)
		}
	}

	anchor := item.Anchor
	if anchor == nil {
		picked, ok := identity.PickAnchor(item.Query, all, catalog.Stores)
		if !ok {
			_, _ = r.Store.Update(id, func(i *store.Item) {
				i.Resolving = false
				i.Matches = nil
				i.Status = status
				now := time.Now().UTC()
				i.ResolvedAt = &now
				i.Error = "ninguna cadena tiene ese producto"
			})
			return nil
		}
		anchor = &picked
	}

	matches := make([]identity.Match, 0, len(catalog.Stores))
	cands := make([]identity.Product, 0, len(catalog.Stores)*r.keep())
	for _, chain := range catalog.Stores {
		hits := byStore[chain]
		if len(hits) == 0 {
			continue
		}
		best, alts := identity.Best(item.Query, *anchor, hits)
		if best.Level == identity.LevelNo {
			continue
		}
		matches = append(matches, best)
		cands = append(cands, best.Product)
		if len(alts) > r.keep() {
			alts = alts[:r.keep()]
		}
		for _, a := range alts {
			cands = append(cands, a.Product)
		}
	}

	now := time.Now().UTC()
	_, err = r.Store.Update(id, func(i *store.Item) {
		i.Resolving = false
		i.Error = ""
		i.Anchor = anchor
		if i.Section == "" {
			i.Section = identity.Section(identity.Domain(picked(anchor)))
		}
		i.Matches = matches
		i.Candidates = cands
		i.Status = status
		i.ResolvedAt = &now
	})
	return err
}

// Retry enqueues every item that failed or that has a chain which could answer later. It
// is the only way the retry button touches the engine.
func (r *Resolver) Retry() int {
	n := 0
	for _, it := range r.Store.Snapshot().Items {
		if it.Error != "" || retryable(it) {
			r.Enqueue(it.ID)
			n++
		}
	}
	return n
}

func retryable(it store.Item) bool {
	for _, code := range it.Status {
		switch code {
		case "quota", "timeout", "network", "blocked":
			return true
		}
	}
	return false
}

// picked dereferences the anchor the update closure needs; an anchor always exists here.
func picked(p *identity.Product) identity.Product {
	if p == nil {
		return identity.Product{}
	}
	return *p
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (r *Resolver) limit() int {
	if r.Limit <= 0 {
		return DefaultLimit
	}
	return r.Limit
}

func (r *Resolver) keep() int {
	if r.Keep <= 0 {
		return DefaultKeep
	}
	return r.Keep
}
