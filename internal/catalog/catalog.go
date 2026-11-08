// Package catalog reads the catalogue engine. The seven supermarket clients live in the
// multisuper repository, which already resolves each chain's quirks (ALDI regions,
// Mercadona warehouses, Alcampo's WAF, Carrefour's third-party feed) and returns one
// document per query with a machine-readable error per chain. Duplicating those clients
// here would mean maintaining the fragile part of the system twice.
//
// The price is converted to integer cents once, at this boundary: every total after this
// point is int64 arithmetic, so 0.1 + 0.2 is 30 and not 0.30000000000000004.
package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/yerbajo/shopping-list-comparator/internal/identity"
)

// Stores are the chains the engine serves, in the order the app shows them.
var Stores = []string{"dia", "mercadona", "mas", "aldi", "lidl", "carrefour", "alcampo"}

// Error is one chain's failure, with the stable code the engine already publishes.
type Error struct {
	Code      string `json:"code"`
	Status    int    `json:"status,omitempty"`
	Retryable bool   `json:"retryable"`
	Message   string `json:"message"`
}

// ChainResult is what one chain answered: its products and its own error, never both.
type ChainResult struct {
	Store string
	Hits  []identity.Product
	Err   *Error
}

// Reply is one search across the chains the caller asked for.
type Reply struct {
	Query   string
	Results []ChainResult
}

// ByStore indexes the reply.
func (r Reply) ByStore() map[string]ChainResult {
	m := make(map[string]ChainResult, len(r.Results))
	for _, c := range r.Results {
		m[c.Store] = c
	}
	return m
}

// Searcher is the engine as the app uses it. Tests replace it with a fake.
type Searcher interface {
	Search(ctx context.Context, term string, limit int, stores []string) (Reply, error)
}

// Client is the HTTP client of multisuper's own web API.
type Client struct {
	base string
	hc   *http.Client
}

// New builds a client for an engine base URL such as http://127.0.0.1:8099.
func New(base string) *Client {
	return &Client{
		base: strings.TrimRight(base, "/"),
		hc:   &http.Client{Timeout: 90 * time.Second},
	}
}

type searchRequest struct {
	Term   string   `json:"term"`
	Limit  int      `json:"limit"`
	Stores []string `json:"stores,omitempty"`
}

type document struct {
	Query   string        `json:"query"`
	Count   int           `json:"count"`
	Results []storeResult `json:"results"`
}

type storeResult struct {
	Store string `json:"store"`
	Count int    `json:"count"`
	Error *Error `json:"error,omitempty"`
	Hits  []struct {
		Store    string   `json:"store"`
		ID       string   `json:"id"`
		Name     string   `json:"name"`
		Brand    string   `json:"brand,omitempty"`
		EAN      string   `json:"ean,omitempty"`
		Size     string   `json:"size,omitempty"`
		Price    *float64 `json:"price,omitempty"`
		Category string   `json:"category,omitempty"`
		URL      string   `json:"url,omitempty"`
		Image    string   `json:"image,omitempty"`
	} `json:"hits"`
}

// Search asks the engine for one term across the chains.
func (c *Client) Search(ctx context.Context, term string, limit int, stores []string) (Reply, error) {
	body, err := json.Marshal(searchRequest{Term: term, Limit: limit, Stores: stores})
	if err != nil {
		return Reply{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/search", bytes.NewReader(body))
	if err != nil {
		return Reply{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return Reply{}, fmt.Errorf("motor de catálogo no disponible en %s: %w", c.base, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Reply{}, fmt.Errorf("motor de catálogo en %s: %s", c.base, resp.Status)
	}
	var doc document
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return Reply{}, fmt.Errorf("motor de catálogo en %s: respuesta ilegible: %w", c.base, err)
	}
	out := Reply{Query: doc.Query, Results: make([]ChainResult, 0, len(doc.Results))}
	for _, r := range doc.Results {
		cr := ChainResult{Store: r.Store, Err: r.Error}
		for _, h := range r.Hits {
			p := identity.Product{
				Store:    h.Store,
				ID:       h.ID,
				Name:     h.Name,
				Brand:    h.Brand,
				Size:     h.Size,
				Category: h.Category,
				EAN:      h.EAN,
				URL:      h.URL,
				Image:    h.Image,
			}
			if h.Price != nil {
				p.PriceCents = Cents(*h.Price)
			}
			cr.Hits = append(cr.Hits, p)
		}
		out.Results = append(out.Results, cr)
	}
	return out, nil
}

// Cents converts a published price into integer cents once. Half a cent rounds away from
// zero, the usual money rule.
func Cents(price float64) *int64 {
	v := int64(math.Round(price * 100))
	return &v
}
