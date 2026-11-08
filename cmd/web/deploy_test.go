package main

// The deployment is part of the product: a wrong volume name in the docs makes a backup of an
// empty directory succeed silently. These tests read the delivered files the way a reader
// would and fail when they stop agreeing with the code.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yerbajo/shopping-list-comparator/internal/catalog"
	"github.com/yerbajo/shopping-list-comparator/internal/resolve"
	"github.com/yerbajo/shopping-list-comparator/internal/store"
	"github.com/yerbajo/shopping-list-comparator/web"
)

func TestDeployUsesTheFlagsMainDefines(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	defined := map[string]bool{}
	for _, m := range regexp.MustCompile(`flag\.(?:String|Duration|Int)\("([^"]+)"`).FindAllStringSubmatch(string(src), -1) {
		defined[m[1]] = true
	}
	if len(defined) != 5 {
		t.Fatalf("main.go defines %v, expected addr, multisuper, data, gap, limit", defined)
	}
	compose, err := os.ReadFile("../../docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	items := regexp.MustCompile(`(?m)^\s+- "(-[^"]+)"`).FindAllStringSubmatch(string(compose), -1)
	if len(items) == 0 {
		t.Fatal("docker-compose.yml has no command item to check")
	}
	for _, m := range items {
		arg := m[1]
		name, _, ok := strings.Cut(strings.TrimPrefix(arg, "-"), "=")
		if !ok || strings.ContainsRune(arg, ' ') {
			t.Errorf("compose item %q must be one argv element: write -flag=value, not -flag value", arg)
		}
		if !defined[name] {
			t.Errorf("compose passes -%s, which main.go does not define", name)
		}
	}
}

type countingSearch struct {
	calls int32
	reply catalog.Reply
}

func (c *countingSearch) Search(context.Context, string, int, []string) (catalog.Reply, error) {
	atomic.AddInt32(&c.calls, 1)
	return c.reply, nil
}

// The healthcheck must be a path the server serves and must not spend a chain's quota: a
// supermarket outage cannot be allowed to mark the container unhealthy.
func TestHealthcheckPathServesWithoutSearching(t *testing.T) {
	dockerfile, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	cmd := regexp.MustCompile(`(?m)^\s*CMD\s+(wget[^\n]*)`).FindStringSubmatch(string(dockerfile))
	if cmd == nil {
		t.Fatal("Dockerfile has no HEALTHCHECK wget command")
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "list.json"))
	if err != nil {
		t.Fatal(err)
	}
	engine := &countingSearch{}
	r := resolve.New(engine, st)
	r.Gap = 0
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r.Start(ctx)
	s := &server{o: options{store: st, resolver: r, assets: web.FS, engine: "http://engine.test"}}

	url := regexp.MustCompile(`http://[0-9.]+:[0-9]+(/[^\s>]*)`).FindStringSubmatch(cmd[1])
	if url == nil {
		t.Fatalf("no URL in healthcheck command %q", cmd[1])
	}
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url[1], nil))
	if rec.Code != http.StatusOK {
		t.Errorf("healthcheck %s: got %d, want 200", url[1], rec.Code)
	}
	if n := atomic.LoadInt32(&engine.calls); n != 0 {
		t.Errorf("healthcheck searched %d times", n)
	}
}

func TestDockerignoreKeepsBuildInputs(t *testing.T) {
	raw, err := os.ReadFile("../../.dockerignore")
	if err != nil {
		t.Fatal(err)
	}
	var patterns []string
	for _, line := range strings.Split(string(raw), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			patterns = append(patterns, line)
		}
	}
	needed := []string{"go.mod", "cmd/web/main.go", "cmd/web/server.go", "internal/identity/brands.json"}
	assets, err := os.ReadFile("../../web/assets.go")
	if err != nil {
		t.Fatal(err)
	}
	embed := regexp.MustCompile(`(?m)^//go:embed\s+(.+)$`).FindStringSubmatch(string(assets))
	if embed == nil {
		t.Fatal("web/assets.go has no go:embed line")
	}
	for _, name := range strings.Fields(embed[1]) {
		needed = append(needed, "web/"+name)
	}
	for _, path := range needed {
		for _, p := range patterns {
			for _, seg := range strings.Split(path, "/") {
				if ok, _ := filepath.Match(p, seg); ok {
					t.Errorf(".dockerignore %q excludes %s, which the build needs", p, path)
				}
			}
		}
	}
}

// The docs must not hardcode a volume name: compose prefixes it with the clone's directory,
// and `tar` of a name that does not exist succeeds with an empty archive.
func TestDocsAskForTheRealVolume(t *testing.T) {
	docs, err := os.ReadFile("../../docs/despliegue.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(docs), `docker inspect -f '{{range .Mounts}}{{.Name}}{{end}}'`) {
		t.Error("the docs must read the volume name from the container, not guess it")
	}
	if !strings.Contains(string(docs), `"$VOLUME":/data`) {
		t.Error("the docs must mount $VOLUME on /data")
	}
	for _, m := range regexp.MustCompile(`-v ([A-Za-z0-9_.-]+):/data`).FindAllStringSubmatch(string(docs), -1) {
		if !strings.Contains(m[1], "_lista-data") && m[1] != "lista-data" {
			t.Errorf("docs mount %q, which is neither the compose volume nor the docker-run one", m[1])
		}
	}
	if !strings.Contains(string(docs), "docker compose stop lista") {
		t.Error("restoring over a running app can be overwritten by a request; the docs must stop it first")
	}
	if !strings.Contains(string(docs), "--user \"$(id -u):$(id -g)\"") {
		t.Error("a backup with root leaves a root-owned file in the working tree")
	}
}

func TestAppJSNeverBuildsHTML(t *testing.T) {
	js, err := web.FS.ReadFile("app.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "eval("} {
		if strings.Contains(string(js), bad) {
			t.Errorf("web/app.js uses %s; product names are third-party strings", bad)
		}
	}
	for _, hook := range []string{"button[data-pick]", "button[data-anchor]", ".closest('.item')"} {
		if !strings.Contains(string(js), hook) {
			t.Errorf("web/app.js lost %s from the delegated row actions", hook)
		}
	}
	if !strings.Contains(string(js), "openKeys") {
		t.Error("web/app.js rebuilds the list on every poll and must keep the open details")
	}
}

// A failed write is the server's fault, not the caller's, and its message must not carry the
// container's paths.
func TestAddStorageFailureIsAServerError(t *testing.T) {
	dir := t.TempDir()
	ro := filepath.Join(dir, "ro")
	if err := os.Mkdir(ro, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
	st, err := store.Open(filepath.Join(ro, "list.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := &server{o: options{store: st, resolver: resolve.New(&countingSearch{}, st), engine: "http://engine.test"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/items", strings.NewReader(`{"query":"leche entera"}`))
	s.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 500: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); strings.Contains(body, ro) || strings.Contains(body, "permission denied") {
		t.Errorf("the 500 leaks the storage path: %s", body)
	}
}
