package report

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type storageVerifyingHouseImages struct {
	*memoryHouseImages
	mu sync.Mutex
	id string
}

func (s *storageVerifyingHouseImages) VerifyStorage(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.id == "" {
		s.id = id
		return nil
	}
	if s.id != id {
		return ErrConflict
	}
	return nil
}

func TestHouseImageStorageIdentityRequiresSamePhysicalRoot(t *testing.T) {
	_, _, provider := imageFixture(t)
	store := &storageVerifyingHouseImages{memoryHouseImages: newMemoryHouseImages()}
	root := t.TempDir()
	if _, err := NewHouseImageService(store, provider, root); err != nil {
		t.Fatal(err)
	}
	if _, err := NewHouseImageService(store, provider, root); err != nil {
		t.Fatalf("shared root rejected: %v", err)
	}
	if _, err := NewHouseImageService(store, provider, t.TempDir()); !errors.Is(err, ErrConflict) {
		t.Fatalf("different physical root accepted: %v", err)
	}
}

func TestHouseImageStorageIdentityCreationIsConcurrentAndAtomic(t *testing.T) {
	root := t.TempDir()
	const workers = 16
	ids := make(chan string, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := loadOrCreateHouseStorageID(root)
			ids <- id
			errs <- err
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first string
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatalf("multiple identities: %q != %q", id, first)
		}
	}
	if !isImageHash(first) {
		t.Fatalf("invalid identity %q", first)
	}
	info, err := os.Lstat(filepath.Join(root, houseStorageMarkerName))
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		t.Fatalf("marker permissions: %v %v", info, err)
	}
}

func TestHouseImageStorageIdentityRejectsTamperingAndSymlink(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(string) error
	}{
		{name: "empty", prepare: func(root string) error { return os.WriteFile(filepath.Join(root, houseStorageMarkerName), nil, 0600) }},
		{name: "invalid", prepare: func(root string) error {
			return os.WriteFile(filepath.Join(root, houseStorageMarkerName), []byte("copied-or-corrupt"), 0600)
		}},
		{name: "permissions", prepare: func(root string) error {
			path := filepath.Join(root, houseStorageMarkerName)
			if err := os.WriteFile(path, []byte(strings.Repeat("a", 64)), 0600); err != nil {
				return err
			}
			return os.Chmod(path, 0644)
		}},
		{name: "symlink", prepare: func(root string) error {
			target := filepath.Join(root, "target")
			if err := os.WriteFile(target, []byte(strings.Repeat("a", 64)), 0600); err != nil {
				return err
			}
			return os.Symlink(target, filepath.Join(root, houseStorageMarkerName))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := tc.prepare(root); err != nil {
				t.Fatal(err)
			}
			if _, err := loadOrCreateHouseStorageID(root); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("tampered marker accepted: %v", err)
			}
		})
	}
}

type storageIDSQLState struct {
	mu      sync.Mutex
	id      string
	queries []string
}
type storageIDSQLDriver struct{ state *storageIDSQLState }
type storageIDSQLConn struct{ state *storageIDSQLState }
type storageIDSQLRows struct {
	id   string
	done bool
}
type storageIDSQLTx struct{}

var storageIDSQLDriverID atomic.Uint64

func (d storageIDSQLDriver) Open(string) (driver.Conn, error) {
	return &storageIDSQLConn{state: d.state}, nil
}
func (c *storageIDSQLConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("not implemented")
}
func (c *storageIDSQLConn) Close() error              { return nil }
func (c *storageIDSQLConn) Begin() (driver.Tx, error) { return storageIDSQLTx{}, nil }
func (storageIDSQLTx) Commit() error                  { return nil }
func (storageIDSQLTx) Rollback() error                { return nil }
func (r *storageIDSQLRows) Columns() []string         { return []string{"storage_id"} }
func (r *storageIDSQLRows) Close() error              { return nil }
func (r *storageIDSQLRows) Next(dest []driver.Value) error {
	if r.done || r.id == "" {
		return io.EOF
	}
	dest[0] = r.id
	r.done = true
	return nil
}
func (c *storageIDSQLConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	c.state.queries = append(c.state.queries, query)
	if len(args) != 1 {
		return nil, errors.New("missing storage identity")
	}
	if c.state.id == "" {
		c.state.id = args[0].Value.(string)
	}
	return driver.RowsAffected(1), nil
}
func (c *storageIDSQLConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	c.state.queries = append(c.state.queries, query)
	return &storageIDSQLRows{id: c.state.id}, nil
}

func TestMySQLHouseImageStorageIdentityIsWriteOnce(t *testing.T) {
	state := &storageIDSQLState{}
	name := fmt.Sprintf("house-storage-id-%d", storageIDSQLDriverID.Add(1))
	sql.Register(name, storageIDSQLDriver{state: state})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store, _ := NewMySQLHouseImageStore(db)
	first := strings.Repeat("a", 64)
	if err := store.VerifyStorage(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if err := store.VerifyStorage(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if err := store.VerifyStorage(context.Background(), strings.Repeat("b", 64)); !errors.Is(err, ErrConflict) {
		t.Fatalf("database identity changed: %v", err)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.id != first || len(state.queries) != 6 || !strings.Contains(state.queries[0], "ON DUPLICATE KEY UPDATE storage_id=storage_id") {
		t.Fatalf("unexpected storage contract: id=%s queries=%v", state.id, state.queries)
	}
}
