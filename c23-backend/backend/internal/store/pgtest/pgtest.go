// Package pgtest provides a real PostgreSQL server for tests.
//
// If $TEST_DATABASE_URL is set (CI service container / docker compose) it is used directly.
// Otherwise an embedded PostgreSQL (real server binaries, fergusstrange/embedded-postgres) is started once per test binary.
package pgtest

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"

	"c23desk/internal/store"
)

var (
	once  sync.Once
	url   string
	pg    *embeddedpostgres.EmbeddedPostgres
	start error
)

func freePort() uint32 {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 55432
	}
	defer l.Close()
	return uint32(l.Addr().(*net.TCPAddr).Port)
}

// URL returns a connection string to a running test database (starting one if needed).
func URL(t testing.TB) string {
	t.Helper()
	once.Do(func() {
		if u := os.Getenv("TEST_DATABASE_URL"); u != "" {
			url = u
			return
		}
		port := freePort()
		dir, err := os.MkdirTemp("", "c23pg")
		if err != nil {
			start = err
			return
		}
		cache, _ := os.UserCacheDir()
		pg = embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().Port(port).Database("c23").Username("c23").Password("c23test").
			RuntimePath(filepath.Join(dir, "runtime")).DataPath(filepath.Join(dir, "data")).
			CachePath(filepath.Join(cache, "c23desk-embedded-postgres")).Logger(nil))
		if start = pg.Start(); start == nil {
			url = fmt.Sprintf("postgres://c23:c23test@127.0.0.1:%d/c23?sslmode=disable", port)
		}
	})
	if start != nil {
		t.Fatalf("cannot start test PostgreSQL: %v", start)
	}
	return url
}

// Stop shuts down the embedded server (call from TestMain).
func Stop() {
	if pg != nil {
		_ = pg.Stop()
	}
}

// Fresh returns a migrated, empty store.
func Fresh(t testing.TB) *store.Store {
	t.Helper()
	ctx := context.Background()
	s, err := store.Open(ctx, URL(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}
