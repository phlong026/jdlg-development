package tracking

import (
	"context"
	"errors"
	"strings"
	"testing"
)

var errMediaFinalize = errors.New("media finalize failed")

type mediaJournalStore struct {
	*memoryStore
	failFinalize    bool
	commitThenError bool
	cancelOnCommit  context.CancelFunc
}

func (s *mediaJournalStore) Transaction(ctx context.Context, tenant string, fn func(Repository) error) error {
	err := s.memoryStore.Transaction(ctx, tenant, func(repo Repository) error {
		if s.failFinalize {
			return fn(mediaJournalRepo{Repository: repo, failFinalize: true})
		}
		return fn(repo)
	})
	if err == nil && s.commitThenError {
		s.commitThenError = false
		if s.cancelOnCommit != nil {
			s.cancelOnCommit()
		}
		return errMediaFinalize
	}
	return err
}

func (s *mediaJournalStore) ReadTransaction(ctx context.Context, tenant string, fn func(Repository) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.memoryStore.ReadTransaction(ctx, tenant, fn)
}

type mediaJournalRepo struct {
	Repository
	failFinalize bool
}

func (r mediaJournalRepo) Insert(rec Record) error {
	if r.failFinalize && rec.Kind == "media" {
		return errMediaFinalize
	}
	return r.Repository.Insert(rec)
}

func journalFixture(t *testing.T) (*Service, Scope, *mediaJournalStore) {
	t.Helper()
	service, scope := fixture(t)
	store := &mediaJournalStore{memoryStore: service.Store.(*memoryStore)}
	service.Store = store
	return service, scope, store
}

func journalAsset() Asset {
	id := "0123456789abcdefghijklmnopqrstuv"
	return Asset{ID: id, Kind: "image", URL: "/api/v1/miniprogram/tracking/media/" + id, Width: 1200, Height: 800, MIME: "image/jpeg", Title: "source.jpg"}
}

func TestFinalizeMediaObjectFailureKeepsPendingAndMediaPrivate(t *testing.T) {
	service, scope, store := journalFixture(t)
	asset := journalAsset()
	op, already, err := service.PrepareMediaObject(context.Background(), scope, asset, "owner-a", strings.Repeat("a", 64), "upload")
	if err != nil || already || op == "" {
		t.Fatalf("prepare = %q %v %v", op, already, err)
	}

	store.failFinalize = true
	if _, err = service.FinalizeMediaObject(context.Background(), scope, asset.ID, op); !errors.Is(err, errMediaFinalize) {
		t.Fatalf("finalize error = %v", err)
	}
	if _, ok := store.data[scope.Tenant]["media_pending:"+asset.ID]; !ok {
		t.Fatal("pending journal was removed after failed finalize")
	}
	if _, ok := store.data[scope.Tenant]["media:"+asset.ID]; ok {
		t.Fatal("media became readable after failed finalize")
	}
	if _, err = service.Media(context.Background(), &scope, asset.ID); err == nil {
		t.Fatal("failed media finalize became readable")
	}
}

func TestFinalizeMediaObjectRecoversCommittedTransactionError(t *testing.T) {
	service, scope, store := journalFixture(t)
	asset := journalAsset()
	ctx, cancel := context.WithCancel(context.Background())
	op, _, err := service.PrepareMediaObject(ctx, scope, asset, "owner-a", strings.Repeat("b", 64), "import:wechat")
	if err != nil {
		t.Fatal(err)
	}

	store.commitThenError = true
	store.cancelOnCommit = cancel
	got, err := service.FinalizeMediaObject(ctx, scope, asset.ID, op)
	if err != nil || got != asset {
		t.Fatalf("finalize = %+v %v", got, err)
	}
	if _, ok := store.data[scope.Tenant]["media_pending:"+asset.ID]; ok {
		t.Fatal("committed pending journal still exists")
	}
}

func TestPrepareMediaObjectIgnoresMutableTitleButNotDimensions(t *testing.T) {
	service, scope, _ := journalFixture(t)
	asset := journalAsset()
	hash := strings.Repeat("2", 64)
	op1, _, err := service.PrepareMediaObject(context.Background(), scope, asset, "owner-a", hash, "import:xhs")
	if err != nil {
		t.Fatal(err)
	}

	renamed := asset
	renamed.Title = "new source caption.jpg"
	op2, already, err := service.PrepareMediaObject(context.Background(), scope, renamed, "owner-a", hash, "import:xhs")
	if err != nil || already || op2 != op1 {
		t.Fatalf("renamed prepare = %q %v %v, want %q false nil", op2, already, err, op1)
	}
	got, err := service.FinalizeMediaObject(context.Background(), scope, asset.ID, op2)
	if err != nil || got != asset {
		t.Fatalf("finalize = %+v %v, want original metadata %+v", got, err, asset)
	}
	if op3, already, err := service.PrepareMediaObject(context.Background(), scope, renamed, "owner-a", hash, "import:xhs"); err != nil || !already || op3 != op1 {
		t.Fatalf("renamed finalized prepare = %q %v %v, want %q true nil", op3, already, err, op1)
	}

	changedDimensions := asset
	changedDimensions.Width++
	if _, _, err := service.PrepareMediaObject(context.Background(), scope, changedDimensions, "owner-a", hash, "import:xhs"); err == nil {
		t.Fatal("same object ID and hash accepted changed dimensions")
	}
}

func TestFinalizeMediaObjectTwoJobsConvergeAfterOneFailure(t *testing.T) {
	service, scope, store := journalFixture(t)
	asset := journalAsset()
	hash := strings.Repeat("c", 64)
	op1, _, err := service.PrepareMediaObject(context.Background(), scope, asset, "owner-a", hash, "import:xhs")
	if err != nil {
		t.Fatal(err)
	}
	op2, _, err := service.PrepareMediaObject(context.Background(), scope, asset, "owner-a", hash, "import:xhs")
	if err != nil || op2 != op1 {
		t.Fatalf("second prepare = %q want %q, err %v", op2, op1, err)
	}

	store.failFinalize = true
	if _, err = service.FinalizeMediaObject(context.Background(), scope, asset.ID, op1); !errors.Is(err, errMediaFinalize) {
		t.Fatalf("first finalize error = %v", err)
	}
	store.failFinalize = false
	if got, err := service.FinalizeMediaObject(context.Background(), scope, asset.ID, op2); err != nil || got != asset {
		t.Fatalf("second finalize = %+v %v", got, err)
	}
	if got, err := service.FinalizeMediaObject(context.Background(), scope, asset.ID, op1); err != nil || got != asset {
		t.Fatalf("idempotent finalize = %+v %v", got, err)
	}
	if op3, already, err := service.PrepareMediaObject(context.Background(), scope, asset, "owner-a", hash, "import:xhs"); err != nil || !already || op3 != op1 {
		t.Fatalf("finalized prepare = %q %v %v", op3, already, err)
	}
	audits := 0
	for _, rec := range store.data[scope.Tenant] {
		if rec.Kind == "audit" {
			audits++
		}
	}
	if audits != 1 {
		t.Fatalf("finalize audit count = %d, want 1", audits)
	}
}

func TestPrepareMediaObjectReusesPendingAcrossRestart(t *testing.T) {
	service, scope, store := journalFixture(t)
	asset := journalAsset()
	hash := strings.Repeat("d", 64)
	op1, _, err := service.PrepareMediaObject(context.Background(), scope, asset, "owner-a", hash, "upload")
	if err != nil {
		t.Fatal(err)
	}
	restarted := New(store, service.Config)
	restarted.Now = service.Now
	op2, already, err := restarted.PrepareMediaObject(context.Background(), scope, asset, "owner-a", hash, "upload")
	if err != nil || already || op2 != op1 {
		t.Fatalf("restart prepare = %q %v %v, want %q false nil", op2, already, err, op1)
	}
}

func TestPrepareMediaObjectRejectsSameIDDifferentHash(t *testing.T) {
	service, scope, _ := journalFixture(t)
	asset := journalAsset()
	if _, _, err := service.PrepareMediaObject(context.Background(), scope, asset, "owner-a", strings.Repeat("e", 64), "upload"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.PrepareMediaObject(context.Background(), scope, asset, "owner-a", strings.Repeat("f", 64), "upload"); err == nil {
		t.Fatal("same media ID accepted a different object hash")
	} else if apiErr, ok := err.(*Error); !ok || apiErr.Status != 409 {
		t.Fatalf("conflict error = %#v", err)
	}
}

func TestFinalizeMediaObjectRequiresCurrentOwnerGrant(t *testing.T) {
	service, scope, _ := journalFixture(t)
	asset := journalAsset()
	op, _, err := service.PrepareMediaObject(context.Background(), scope, asset, "owner-a", strings.Repeat("1", 64), "upload")
	if err != nil {
		t.Fatal(err)
	}
	denied := scope
	denied.Owners = []string{"owner-b"}
	if _, err = service.FinalizeMediaObject(context.Background(), denied, asset.ID, op); err == nil {
		t.Fatal("finalize accepted a scope without the current owner grant")
	}
	if _, err = service.FinalizeMediaObject(context.Background(), scope, asset.ID, op); err != nil {
		t.Fatalf("authorized finalize failed after denied attempt: %v", err)
	}
}
