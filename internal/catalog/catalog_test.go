package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSearchDecodesChainsCentsAndErrors(t *testing.T) {
	var got searchRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/search" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
		  "query": "leche entera",
		  "count": 2,
		  "results": [
		    {"store": "dia", "count": 2, "elapsedMs": 300, "hits": [
		      {"store": "dia", "id": "1", "name": "Leche entera Dia Láctea 1 L", "brand": "Dia Láctea", "size": "1 L", "price": 0.965, "category": "Leche", "url": "https://www.dia.es/p/1"},
		      {"store": "dia", "id": "2", "name": "Leche entera sin lactosa Dia Láctea 1 L", "brand": "Dia Láctea", "price": 0.94}
		    ]},
		    {"store": "alcampo", "count": 0, "elapsedMs": 276,
		     "error": {"code": "quota", "status": 202, "retryable": true, "message": "AWS WAF challenge"},
		     "hits": []}
		  ]
		}`))
	}))
	defer srv.Close()

	reply, err := New(srv.URL+"/").Search(context.Background(), "leche entera", 20, []string{"dia", "alcampo"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if got.Term != "leche entera" || got.Limit != 20 || len(got.Stores) != 2 {
		t.Errorf("request sent to the engine: %+v", got)
	}
	if reply.Query != "leche entera" || len(reply.Results) != 2 {
		t.Fatalf("reply: %+v", reply)
	}
	by := reply.ByStore()
	dia := by["dia"]
	if len(dia.Hits) != 2 {
		t.Fatalf("dia hits: %d", len(dia.Hits))
	}
	if p := dia.Hits[0]; p.PriceCents == nil || *p.PriceCents != 97 {
		t.Errorf("cents: got %v, want 97", p.PriceCents)
	}
	if c := dia.Hits[1].PriceCents; c == nil || *c != 94 {
		t.Errorf("second price: got %v, want 94", c)
	}
	if dia.Hits[0].URL == "" || dia.Hits[0].Category != "Leche" {
		t.Errorf("fields dropped: %+v", dia.Hits[0])
	}
	alc := by["alcampo"]
	if alc.Err == nil || alc.Err.Code != "quota" || !alc.Err.Retryable || alc.Err.Status != 202 {
		t.Errorf("chain error not preserved: %+v", alc.Err)
	}
	if len(alc.Hits) != 0 {
		t.Errorf("blocked chain returned hits: %+v", alc.Hits)
	}
}

func TestSearchReportsAnUnreachableEngine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer srv.Close()

	if _, err := New(srv.URL).Search(context.Background(), "arroz", 10, nil); err == nil {
		t.Fatal("a broken engine must be an error, not an empty list")
	}
}

func TestCents(t *testing.T) {
	for price, want := range map[float64]int64{0.96: 96, 5.76: 576, 0.965: 97, 0: 0, 1.005: 100} {
		if got := *Cents(price); got != want {
			t.Errorf("Cents(%v) = %d, want %d", price, got, want)
		}
	}
}
