package report

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type memoryHouseImages struct {
	mu           sync.Mutex
	rows         map[string]HouseImageRecord
	owner        int64
	lease        string
	failComplete bool
}

func newMemoryHouseImages() *memoryHouseImages {
	return &memoryHouseImages{rows: map[string]HouseImageRecord{}, owner: 7, lease: "lease-1"}
}
func (s *memoryHouseImages) Get(_ context.Context, r *Report, id string) (HouseImageRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.UserID != s.owner {
		return HouseImageRecord{}, ErrNotFound
	}
	row, ok := s.rows[r.ID+":"+id]
	if !ok {
		return row, ErrNotFound
	}
	return row, nil
}
func (s *memoryHouseImages) Reserve(_ context.Context, r *Report, row HouseImageRecord) (HouseImageRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.UserID != s.owner || r.LeaseToken != s.lease {
		return HouseImageRecord{}, false, ErrConflict
	}
	key := r.ID + ":" + row.Task.ID
	if old, ok := s.rows[key]; ok {
		return old, false, nil
	}
	s.rows[key] = row
	return row, true, nil
}
func (s *memoryHouseImages) Complete(_ context.Context, r *Report, row HouseImageRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.UserID != s.owner || r.LeaseToken != s.lease || s.failComplete {
		return ErrConflict
	}
	key := r.ID + ":" + row.Task.ID
	old, ok := s.rows[key]
	if !ok || old.RequestHash != row.RequestHash {
		return ErrConflict
	}
	row.Status = "complete"
	s.rows[key] = row
	return nil
}

type fakeHouseImageProvider struct {
	mu       sync.Mutex
	calls    int
	data     []byte
	err      error
	ref      []byte
	entered  chan struct{}
	release  chan struct{}
	identity HouseImageProviderIdentity
}

func (p *fakeHouseImageProvider) Identity() HouseImageProviderIdentity { return p.identity }
func (p *fakeHouseImageProvider) Generate(ctx context.Context, _ string, ref []byte) (HouseGeneratedImage, error) {
	p.mu.Lock()
	p.calls++
	p.ref = append([]byte(nil), ref...)
	p.mu.Unlock()
	if p.entered != nil {
		p.entered <- struct{}{}
		select {
		case <-p.release:
		case <-ctx.Done():
			return HouseGeneratedImage{}, ctx.Err()
		}
	}
	return HouseGeneratedImage{Data: p.data}, p.err
}
func imageFixture(t *testing.T) (*Report, HouseImageTask, *fakeHouseImageProvider) {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2048, 1152))); err != nil {
		t.Fatal(err)
	}
	return &Report{ID: "house-report", UserID: 7, InputHash: strings.Repeat("a", 64), ConfirmationHash: strings.Repeat("b", 64), ProtocolVersion: ProtocolHouseV1, LeaseToken: "lease-1", Paid: true, Status: StatusFullGenerating}, HouseImageTask{ID: "a-floor-1", SchemeID: "A", SchemeVersion: "v1", Kind: "floor_plan", Floor: 1, Prompt: "一层概念平面"}, &fakeHouseImageProvider{data: b.Bytes(), identity: HouseImageProviderIdentity{Provider: "test", Model: "test-image", ConfigVersion: "cfg-v1"}}
}
func TestHouseImagesCacheReferenceAndPrivateFiles(t *testing.T) {
	r, task, p := imageFixture(t)
	s := newMemoryHouseImages()
	root := t.TempDir()
	images, err := NewHouseImageService(s, p, root)
	if err != nil {
		t.Fatal(err)
	}
	a, created, err := images.Ensure(context.Background(), r, task)
	if err != nil || !created {
		t.Fatalf("first %v %v", created, err)
	}
	again, created, err := images.Ensure(context.Background(), r, task)
	if err != nil || created || a != again || p.calls != 1 {
		t.Fatalf("cache %v %v calls=%d", created, err, p.calls)
	}
	task.ID = "a-volume"
	task.Kind = "massing"
	task.Floor = 0
	task.ReferenceID = a.ID
	if _, _, err := images.Ensure(context.Background(), r, task); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(p.ref, p.data) {
		t.Fatal("reference not propagated")
	}
	raw, err := images.Read(context.Background(), r, a)
	if err != nil || !bytes.Equal(raw, p.data) {
		t.Fatal("read", err)
	}
	info, err := os.Stat(filepath.Join(root, a.Asset))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("file permissions", err)
	}
	if err := filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && info.Mode().Perm() != 0700 {
			return errors.New("directory permissions")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
func TestHouseImagesUnknownSubmissionNeverResubmits(t *testing.T) {
	r, task, p := imageFixture(t)
	p.err = errors.New("provider timeout")
	s := newMemoryHouseImages()
	root := t.TempDir()
	images, _ := NewHouseImageService(s, p, root)
	for i := 0; i < 2; i++ {
		_, _, err := images.Ensure(context.Background(), r, task)
		if !errors.Is(err, ErrImageUncertain) {
			t.Fatalf("error %v", err)
		}
		images, _ = NewHouseImageService(s, p, root)
	}
	if p.calls != 1 {
		t.Fatalf("paid calls %d", p.calls)
	}
}
func TestHouseImagesResumeSavedAssetAfterLeaseLoss(t *testing.T) {
	r, task, p := imageFixture(t)
	s := newMemoryHouseImages()
	s.failComplete = true
	root := t.TempDir()
	images, _ := NewHouseImageService(s, p, root)
	if _, _, err := images.Ensure(context.Background(), r, task); err == nil {
		t.Fatal("expected completion failure")
	}
	s.failComplete = false
	s.lease = "lease-2"
	r.LeaseToken = "lease-2"
	images, _ = NewHouseImageService(s, p, root)
	_, created, err := images.Ensure(context.Background(), r, task)
	if err != nil || created || p.calls != 1 {
		t.Fatalf("resume %v %v calls=%d", created, err, p.calls)
	}
}
func TestHouseImagesIsolationAndReferenceVersion(t *testing.T) {
	r, task, p := imageFixture(t)
	s := newMemoryHouseImages()
	images, _ := NewHouseImageService(s, p, t.TempDir())
	a, _, err := images.Ensure(context.Background(), r, task)
	if err != nil {
		t.Fatal(err)
	}
	other := *r
	other.UserID++
	if _, err := images.Read(context.Background(), &other, a); err == nil {
		t.Fatal("cross-owner read")
	}
	other = *r
	other.ID = "other-report"
	if _, err := images.Read(context.Background(), &other, a); err == nil {
		t.Fatal("cross-report read")
	}
	task.ID = "b-volume"
	task.Kind = "massing"
	task.Floor = 0
	task.SchemeID = "B"
	task.ReferenceID = a.ID
	if _, _, err := images.Ensure(context.Background(), r, task); err == nil {
		t.Fatal("cross-scheme reference")
	}
	task.SchemeID = "A"
	task.SchemeVersion = "v2"
	if _, _, err := images.Ensure(context.Background(), r, task); err == nil {
		t.Fatal("cross-version reference")
	}
	if p.calls != 1 {
		t.Fatal("invalid reference generated")
	}
}
func TestHouseImagesConcurrentSubmissionAndStaleLease(t *testing.T) {
	r, task, p := imageFixture(t)
	s := newMemoryHouseImages()
	p.entered = make(chan struct{}, 1)
	p.release = make(chan struct{})
	images, _ := NewHouseImageService(s, p, t.TempDir())
	done := make(chan error, 1)
	go func() { _, _, err := images.Ensure(context.Background(), r, task); done <- err }()
	<-p.entered
	if _, _, err := images.Ensure(context.Background(), r, task); !errors.Is(err, ErrImageUncertain) {
		t.Fatal("duplicate reservation", err)
	}
	close(p.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	stale := *r
	stale.LeaseToken = "old"
	task.ID = "other"
	if _, _, err := images.Ensure(context.Background(), &stale, task); err == nil {
		t.Fatal("stale lease accepted")
	}
	if p.calls != 1 {
		t.Fatal("duplicate provider call")
	}
}
func TestHouseImagesRejectChangedInputAndTamperedAsset(t *testing.T) {
	r, task, p := imageFixture(t)
	s := newMemoryHouseImages()
	root := t.TempDir()
	images, _ := NewHouseImageService(s, p, root)
	a, _, err := images.Ensure(context.Background(), r, task)
	if err != nil {
		t.Fatal(err)
	}
	task.Prompt = "changed"
	if _, _, err := images.Ensure(context.Background(), r, task); !errors.Is(err, ErrConflict) {
		t.Fatal("prompt not frozen", err)
	}
	p.identity.Model = "different"
	task.Prompt = "一层概念平面"
	changed, _ := NewHouseImageService(s, p, root)
	if _, _, err := changed.Ensure(context.Background(), r, task); !errors.Is(err, ErrConflict) {
		t.Fatal("model not frozen", err)
	}
	bad := a
	bad.Asset = "../outside.png"
	if _, err := images.Read(context.Background(), r, bad); err == nil {
		t.Fatal("path escape")
	}
	if err := os.WriteFile(filepath.Join(root, a.Asset), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := images.Read(context.Background(), r, a); err == nil {
		t.Fatal("tampered image")
	}
}

func TestHouseImagesInvalidProviderImageStaysUncertain(t *testing.T) {
	r, task, p := imageFixture(t)
	p.data = []byte("not an image")
	s := newMemoryHouseImages()
	images, err := NewHouseImageService(s, p, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, _, err := images.Ensure(context.Background(), r, task); !errors.Is(err, ErrImageUncertain) {
			t.Fatalf("invalid image error %v", err)
		}
	}
	if p.calls != 1 {
		t.Fatalf("paid request repeated %d", p.calls)
	}
}

func TestHouseImagesJPEGAndSymlinkIsolation(t *testing.T) {
	r, task, p := imageFixture(t)
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2048, 1152)), nil); err != nil {
		t.Fatal(err)
	}
	p.data = encoded.Bytes()
	root := t.TempDir()
	images, err := NewHouseImageService(newMemoryHouseImages(), p, root)
	if err != nil {
		t.Fatal(err)
	}
	a, _, err := images.Ensure(context.Background(), r, task)
	if err != nil || !strings.HasSuffix(a.Asset, ".jpg") {
		t.Fatalf("JPEG %q %v", a.Asset, err)
	}
	if _, err := images.Read(context.Background(), r, a); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, a.Asset)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.jpg")
	if err := os.WriteFile(outside, p.data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if _, err := images.Read(context.Background(), r, a); err == nil {
		t.Fatal("symlink asset accepted")
	}
}
