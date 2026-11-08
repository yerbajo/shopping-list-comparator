package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yerbajo/shopping-list-comparator/internal/catalog"
	"github.com/yerbajo/shopping-list-comparator/internal/resolve"
	"github.com/yerbajo/shopping-list-comparator/internal/store"
	"github.com/yerbajo/shopping-list-comparator/web"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8100", "dirección donde sirve la web")
	engine := flag.String("multisuper", "http://127.0.0.1:8099", "URL del motor de catálogo (multisuper)")
	data := flag.String("data", "data/list.json", "fichero de la lista")
	gap := flag.Duration("gap", resolve.DefaultGap, "pausa entre búsquedas al motor")
	limit := flag.Int("limit", resolve.DefaultLimit, "productos pedidos a cada cadena por item")
	flag.Parse()

	st, err := store.Open(*data)
	if err != nil {
		log.Fatalf("lista: %v", err)
	}
	r := resolve.New(catalog.New(*engine), st)
	r.Gap, r.Limit = *gap, *limit

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	r.Start(ctx)

	// Finish what a previous run left half done instead of leaving rows stuck on "buscando".
	queued := 0
	for _, it := range st.Snapshot().Items {
		if it.ResolvedAt == nil {
			r.Enqueue(it.ID)
			queued++
		}
	}

	s := &server{o: options{store: st, resolver: r, assets: web.FS, engine: *engine}}
	srv := &http.Server{Addr: *addr, Handler: s.handler(), ReadHeaderTimeout: 10 * time.Second}

	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	log.Printf("lista en http://%s · motor %s · %s · %d pendientes", *addr, *engine, st.Path(), queued)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
