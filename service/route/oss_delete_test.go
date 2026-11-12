package route

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"

	"service/config"
	"service/model"

	"github.com/tencentyun/cos-go-sdk-v5"
)

type fakeOSSDeleteStore struct {
	files      map[int64]*model.OSSFile
	deleteErrs map[int64]error
	deleteIDs  []int64
}

func (s *fakeOSSDeleteStore) GetByID(id int64) (*model.OSSFile, error) {
	file, ok := s.files[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	copy := *file
	return &copy, nil
}

func (s *fakeOSSDeleteStore) Delete(id int64) error {
	s.deleteIDs = append(s.deleteIDs, id)
	if err := s.deleteErrs[id]; err != nil {
		return err
	}
	delete(s.files, id)
	return nil
}

func TestDeleteManagedOSSFilePreservesRecordWhenStorageUnavailable(t *testing.T) {
	store := newFakeOSSDeleteStore(1)

	err := deleteManagedOSSFile(context.Background(), store, nil, 1, time.Second)

	if !errors.Is(err, errOSSStorageUnavailable) {
		t.Fatalf("expected storage unavailable, got %v", err)
	}
	if len(store.deleteIDs) != 0 || store.files[1] == nil {
		t.Fatalf("database record must be preserved: calls=%v files=%v", store.deleteIDs, store.files)
	}
}

func TestDeleteManagedOSSFilePreservesRecordWhenObjectDeleteFails(t *testing.T) {
	store := newFakeOSSDeleteStore(1)
	deleteErr := errors.New("cos unavailable")

	err := deleteManagedOSSFile(context.Background(), store, func(context.Context, *model.OSSFile) error {
		return deleteErr
	}, 1, time.Second)

	if !errors.Is(err, deleteErr) {
		t.Fatalf("expected object delete error, got %v", err)
	}
	if len(store.deleteIDs) != 0 || store.files[1] == nil {
		t.Fatalf("database record must be preserved: calls=%v files=%v", store.deleteIDs, store.files)
	}
}

func TestDeleteManagedOSSFileTreatsObject404AsIdempotent(t *testing.T) {
	store := newFakeOSSDeleteStore(1)

	err := deleteManagedOSSFile(context.Background(), store, func(context.Context, *model.OSSFile) error {
		return errOSSObjectNotFound
	}, 1, time.Second)

	if err != nil {
		t.Fatalf("404 should allow database cleanup, got %v", err)
	}
	if !reflect.DeepEqual(store.deleteIDs, []int64{1}) || store.files[1] != nil {
		t.Fatalf("expected record cleanup: calls=%v files=%v", store.deleteIDs, store.files)
	}
}

func TestDeleteManagedOSSFileDatabaseFailureCanBeRetried(t *testing.T) {
	store := newFakeOSSDeleteStore(1)
	store.deleteErrs[1] = errors.New("db temporarily unavailable")
	deleteCalls := 0
	deleter := func(context.Context, *model.OSSFile) error {
		deleteCalls++
		if deleteCalls == 1 {
			return nil
		}
		return errOSSObjectNotFound
	}

	if err := deleteManagedOSSFile(context.Background(), store, deleter, 1, time.Second); err == nil {
		t.Fatal("expected first database delete to fail")
	}
	if store.files[1] == nil {
		t.Fatal("failed database delete must leave the record for retry")
	}
	delete(store.deleteErrs, 1)
	if err := deleteManagedOSSFile(context.Background(), store, deleter, 1, time.Second); err != nil {
		t.Fatalf("retry should accept COS 404 and clean the record: %v", err)
	}
	if store.files[1] != nil || deleteCalls != 2 {
		t.Fatalf("retry did not converge: files=%v deleteCalls=%d", store.files, deleteCalls)
	}
}

func TestDeleteManagedOSSFileTimesOutWithoutDeletingRecord(t *testing.T) {
	store := newFakeOSSDeleteStore(1)

	err := deleteManagedOSSFile(context.Background(), store, func(ctx context.Context, _ *model.OSSFile) error {
		<-ctx.Done()
		return ctx.Err()
	}, 1, 5*time.Millisecond)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
	if len(store.deleteIDs) != 0 || store.files[1] == nil {
		t.Fatal("timeout must preserve database record")
	}
}

func TestDeleteManagedOSSFilesOnlyDeletesSuccessfulItems(t *testing.T) {
	store := newFakeOSSDeleteStore(1, 2, 3)
	result := deleteManagedOSSFiles(context.Background(), store, func(_ context.Context, file *model.OSSFile) error {
		switch file.ObjectKey {
		case "objects/2":
			return errors.New("cos unavailable")
		case "objects/3":
			return errOSSObjectNotFound
		default:
			return nil
		}
	}, []int64{1, 2, 3}, time.Second)

	if !reflect.DeepEqual(result.DeletedIDs, []int64{1, 3}) {
		t.Fatalf("unexpected deleted ids: %v", result.DeletedIDs)
	}
	if len(result.Failures) != 1 || result.Failures[0].ID != 2 || result.Failures[0].Reason != ossDeleteReasonObjectFailed {
		t.Fatalf("unexpected failures: %+v", result.Failures)
	}
	if store.files[1] != nil || store.files[3] != nil || store.files[2] == nil {
		t.Fatalf("partial failure removed wrong records: %v", store.files)
	}
}

func TestDeleteManagedOSSFilesTreatsMissingRecordAsIdempotent(t *testing.T) {
	store := newFakeOSSDeleteStore(2)
	result := deleteManagedOSSFiles(context.Background(), store, func(context.Context, *model.OSSFile) error {
		return nil
	}, []int64{1, 2}, time.Second)

	if !reflect.DeepEqual(result.DeletedIDs, []int64{2}) || !reflect.DeepEqual(result.AlreadyMissingIDs, []int64{1}) {
		t.Fatalf("unexpected idempotent result: %+v", result)
	}
	if len(result.Failures) != 0 {
		t.Fatalf("missing records must not make a retried batch fail: %+v", result.Failures)
	}
}

func TestNormalizeCOSObjectDeleteErrorMapsWrapped404(t *testing.T) {
	responseErr := &cos.ErrorResponse{Response: &http.Response{StatusCode: http.StatusNotFound}}
	err := normalizeCOSObjectDeleteError(fmt.Errorf("wrapped: %w", responseErr))
	if !errors.Is(err, errOSSObjectNotFound) {
		t.Fatalf("expected idempotent object-not-found error, got %v", err)
	}
}

func TestNormalizeOSSDeleteIDsDeduplicatesAndEnforcesLimit(t *testing.T) {
	ids, err := normalizeOSSDeleteIDs([]int64{3, 1, 3, 2}, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(ids, []int64{3, 1, 2}) {
		t.Fatalf("unexpected ids: %v", ids)
	}
	if _, err := normalizeOSSDeleteIDs([]int64{1, 2, 3, 4}, 3); !errors.Is(err, errOSSDeleteBatchTooLarge) {
		t.Fatalf("expected batch limit error, got %v", err)
	}
	if _, err := normalizeOSSDeleteIDs([]int64{1, 0}, 3); !errors.Is(err, errOSSDeleteInvalidID) {
		t.Fatalf("expected invalid id error, got %v", err)
	}
}

func TestCOSDeleteUsesConfiguredPrefixForSingleDelete(t *testing.T) {
	client, baseURL, requests, closeServer := newFakeCOSDeleteClient(t)
	defer closeServer()
	store := newFakeOSSDeleteStore(1)
	cfg := &config.Config{COS: config.COSConfig{Prefix: "tenant-assets/", Domain: baseURL}}
	store.files[1].FileURL = baseURL + "/tenant-assets/objects/1"

	if err := deleteManagedOSSFile(context.Background(), store, newCOSObjectDeleteFunc(client, cfg), 1, time.Second); err != nil {
		t.Fatalf("single delete failed: %v", err)
	}
	if !reflect.DeepEqual(*requests, []string{"DELETE /tenant-assets/objects/1"}) {
		t.Fatalf("unexpected COS request: %v", *requests)
	}
}

func TestCOSDeleteBatchPrefixesRelativeKeysWithoutDoublePrefixingHistoricalFullKeys(t *testing.T) {
	client, baseURL, requests, closeServer := newFakeCOSDeleteClient(t)
	defer closeServer()
	store := newFakeOSSDeleteStore(1, 2)
	store.files[2].ObjectKey = "tenant-assets/objects/2"
	cfg := &config.Config{COS: config.COSConfig{Prefix: "changed-prefix/", Domain: baseURL}}
	store.files[1].FileURL = baseURL + "/tenant-assets/objects/1"
	store.files[2].FileURL = baseURL + "/tenant-assets/objects/2"

	result := deleteManagedOSSFiles(context.Background(), store, newCOSObjectDeleteFunc(client, cfg), []int64{1, 2}, time.Second)
	if len(result.Failures) != 0 {
		t.Fatalf("batch delete failed: %+v", result)
	}
	if !reflect.DeepEqual(*requests, []string{"DELETE /tenant-assets/objects/1", "DELETE /tenant-assets/objects/2"}) {
		t.Fatalf("unexpected COS requests: %v", *requests)
	}
}

func TestUploadDatabaseFailureCompensationUsesConfiguredPrefix(t *testing.T) {
	client, baseURL, requests, closeServer := newFakeCOSDeleteClient(t)
	defer closeServer()
	cfg := &config.Config{COS: config.COSConfig{Prefix: "tenant-assets/", Domain: baseURL}}

	if err := compensateUploadedOSSObject(context.Background(), client, cfg, "admin_uploads/file.png", time.Second); err != nil {
		t.Fatalf("compensation delete failed: %v", err)
	}
	if err := compensateUploadedOSSObject(context.Background(), client, cfg, "tenant-assets/admin_uploads/file.png", time.Second); err != nil {
		t.Fatalf("full-key compensation delete failed: %v", err)
	}
	if !reflect.DeepEqual(*requests, []string{
		"DELETE /tenant-assets/admin_uploads/file.png",
		"DELETE /tenant-assets/admin_uploads/file.png",
	}) {
		t.Fatalf("unexpected COS request: %v", *requests)
	}
}

func TestManagedDeleteRejectsUntrustedOrAmbiguousFileURL(t *testing.T) {
	client, baseURL, requests, closeServer := newFakeCOSDeleteClient(t)
	defer closeServer()
	cfg := &config.Config{COS: config.COSConfig{Prefix: "new-prefix/", Domain: baseURL}}
	for name, fileURL := range map[string]string{
		"untrusted host":  "https://untrusted.invalid/old-prefix/objects/1",
		"mismatched path": baseURL + "/old-prefix/different/1",
		"missing URL":     "",
	} {
		t.Run(name, func(t *testing.T) {
			store := newFakeOSSDeleteStore(1)
			store.files[1].FileURL = fileURL
			err := deleteManagedOSSFile(context.Background(), store, newCOSObjectDeleteFunc(client, cfg), 1, time.Second)
			if !errors.Is(err, errOSSObjectKeyAmbiguous) {
				t.Fatalf("expected ambiguous key error, got %v", err)
			}
			if len(*requests) != 0 || store.files[1] == nil || len(store.deleteIDs) != 0 {
				t.Fatalf("ambiguous record must not trigger COS or DB deletion: requests=%v deletes=%v", *requests, store.deleteIDs)
			}
		})
	}
}

func newFakeCOSDeleteClient(t *testing.T) (*cos.Client, string, *[]string, func()) {
	t.Helper()
	requests := make([]string, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	bucketURL, err := url.Parse(server.URL)
	if err != nil {
		server.Close()
		t.Fatalf("parse fake COS URL: %v", err)
	}
	client := cos.NewClient(&cos.BaseURL{BucketURL: bucketURL}, server.Client())
	return client, server.URL, &requests, server.Close
}

func newFakeOSSDeleteStore(ids ...int64) *fakeOSSDeleteStore {
	store := &fakeOSSDeleteStore{
		files:      make(map[int64]*model.OSSFile, len(ids)),
		deleteErrs: make(map[int64]error),
	}
	for _, id := range ids {
		store.files[id] = &model.OSSFile{ID: id, ObjectKey: fmt.Sprintf("objects/%d", id)}
	}
	return store
}
