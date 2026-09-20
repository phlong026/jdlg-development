package report

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const houseImageMaxBytes = 32 << 20

type HouseImageProviderIdentity struct {
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	ConfigVersion string `json:"config_version"`
}
type HouseGeneratedImage struct{ Data []byte }
type HouseImageProvider interface {
	Identity() HouseImageProviderIdentity
	Generate(context.Context, string, []byte) (HouseGeneratedImage, error)
}

// HouseImageRecord is immutable after reservation except for Status and Asset.
// A submitting row never expires into another paid submission.
type HouseImageRecord struct {
	ReportID         string                     `json:"report_id"`
	UserID           int64                      `json:"user_id"`
	InputHash        string                     `json:"input_hash"`
	ConfirmationHash string                     `json:"confirmation_hash"`
	RequestHash      string                     `json:"request_hash"`
	ReferenceHash    string                     `json:"reference_hash,omitempty"`
	Status           string                     `json:"status"`
	Identity         HouseImageProviderIdentity `json:"identity"`
	Task             HouseImageTask             `json:"task"`
	Asset            HouseImageAsset            `json:"asset"`
}

// Reserve and Complete must atomically check the current database report lease
// and owner. Get checks the owner, report and input version without a lease.
type HouseImageStore interface {
	Get(context.Context, *Report, string) (HouseImageRecord, error)
	Reserve(context.Context, *Report, HouseImageRecord) (HouseImageRecord, bool, error)
	Complete(context.Context, *Report, HouseImageRecord) error
}

type PersistentHouseImages struct {
	store    HouseImageStore
	provider HouseImageProvider
	identity HouseImageProviderIdentity
	root     string
}

func NewHouseImageService(store HouseImageStore, provider HouseImageProvider, assetRoot string) (*PersistentHouseImages, error) {
	if store == nil || provider == nil || !filepath.IsAbs(assetRoot) {
		return nil, ErrInvalidInput
	}
	identity := provider.Identity()
	if strings.TrimSpace(identity.Provider) == "" || strings.TrimSpace(identity.Model) == "" || strings.TrimSpace(identity.ConfigVersion) == "" {
		return nil, ErrInvalidInput
	}
	if err := os.MkdirAll(assetRoot, 0700); err != nil {
		return nil, imageIOFailure("create_root", "", err)
	}
	info, err := os.Lstat(assetRoot)
	if err != nil {
		return nil, imageIOFailure("stat_root", "", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrInvalidInput
	}
	if err := os.Chmod(assetRoot, 0700); err != nil {
		return nil, imageIOFailure("protect_root", "", err)
	}
	root, err := filepath.EvalSymlinks(assetRoot)
	if err != nil {
		return nil, imageIOFailure("resolve_root", "", err)
	}
	storageID, err := loadOrCreateHouseStorageID(root)
	if err != nil {
		return nil, err
	}
	if verifier, ok := store.(houseImageStorageVerifier); ok {
		if err := verifier.VerifyStorage(context.Background(), storageID); err != nil {
			return nil, err
		}
	}
	return &PersistentHouseImages{store: store, provider: provider, identity: identity, root: root}, nil
}

func (s *PersistentHouseImages) Ensure(ctx context.Context, r *Report, task HouseImageTask) (HouseImageAsset, bool, error) {
	if ctx == nil || !validHouseImageOwner(r) || !validHouseImageTask(task) || !validIdentifier(r.LeaseToken, 64) {
		return HouseImageAsset{}, false, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return HouseImageAsset{}, false, err
	}
	if s.provider.Identity() != s.identity {
		return HouseImageAsset{}, false, ErrConflict
	}
	var reference []byte
	var referenceHash string
	if task.ReferenceID != "" {
		prior, err := s.store.Get(ctx, r, task.ReferenceID)
		if err != nil {
			return HouseImageAsset{}, false, err
		}
		if !imageRecordBelongsTo(prior, r) || prior.Status != "complete" || prior.Task.SchemeID != task.SchemeID || prior.Task.SchemeVersion != task.SchemeVersion {
			return HouseImageAsset{}, false, ErrConflict
		}
		reference, err = s.Read(ctx, r, prior.Asset)
		if err != nil {
			return HouseImageAsset{}, false, err
		}
		referenceHash = prior.Asset.SHA256
	}
	proposed := HouseImageRecord{ReportID: r.ID, UserID: r.UserID, InputHash: r.InputHash, ConfirmationHash: r.ConfirmationHash, ReferenceHash: referenceHash, Status: "submitting", Identity: s.identity, Task: task}
	fingerprint, err := json.Marshal(proposed)
	if err != nil || len(fingerprint) > 96<<10 {
		return HouseImageAsset{}, false, ErrInvalidInput
	}
	proposed.RequestHash = imageDigest(fingerprint)
	stored, claimed, err := s.store.Reserve(ctx, r, proposed)
	if err != nil {
		return HouseImageAsset{}, false, err
	}
	if !imageRecordBelongsTo(stored, r) || stored.RequestHash != proposed.RequestHash {
		return HouseImageAsset{}, false, ErrConflict
	}
	if !claimed {
		if stored.Status == "complete" {
			if _, err := s.Read(ctx, r, stored.Asset); err != nil {
				return HouseImageAsset{}, false, err
			}
			return stored.Asset, false, nil
		}
		if stored.Status != "submitting" {
			return HouseImageAsset{}, false, ErrConflict
		}
		// A prior lease may have saved the verified image before losing its database
		// lease. Only that durable receipt can complete the row without a new call.
		recovered, err := s.loadReceipt(stored)
		if err != nil {
			return HouseImageAsset{}, false, err
		}
		if err := s.store.Complete(ctx, r, recovered); err != nil {
			return HouseImageAsset{}, false, err
		}
		return recovered.Asset, false, nil
	}
	requestCtx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	result, err := s.provider.Generate(requestCtx, task.Prompt, reference)
	if err != nil {
		log.Printf("[report-image] warn: provider_result_uncertain report_id=%s image_id=%s", r.ID, task.ID)
		return HouseImageAsset{}, false, ErrImageUncertain
	}
	if err := validateHouseImageBytes(result.Data); err != nil {
		log.Printf("[report-image] warn: provider_asset_rejected report_id=%s image_id=%s", r.ID, task.ID)
		return HouseImageAsset{}, false, ErrImageUncertain
	}
	stored.Asset = HouseImageAsset{ID: task.ID, Asset: s.assetName(stored), SchemeID: task.SchemeID, SchemeVersion: task.SchemeVersion, Kind: task.Kind, Floor: task.Floor, Width: 2048, Height: 1152, Model: s.identity.Model, SHA256: imageDigest(result.Data), ReferenceID: task.ReferenceID}
	if _, format, _ := image.DecodeConfig(bytes.NewReader(result.Data)); format == "jpeg" {
		stored.Asset.Asset = strings.TrimSuffix(stored.Asset.Asset, ".png") + ".jpg"
	}
	stored.Status = "complete"
	if err := s.saveReceipt(stored, result.Data); err != nil {
		return HouseImageAsset{}, false, err
	}
	if err := s.store.Complete(ctx, r, stored); err != nil {
		return HouseImageAsset{}, false, err
	}
	return stored.Asset, true, nil
}

func (s *PersistentHouseImages) Read(ctx context.Context, r *Report, asset HouseImageAsset) ([]byte, error) {
	if ctx == nil || !validHouseImageOwner(r) || !validIdentifier(asset.ID, 80) {
		return nil, ErrInvalidInput
	}
	stored, err := s.store.Get(ctx, r, asset.ID)
	if err != nil {
		return nil, err
	}
	if !imageRecordBelongsTo(stored, r) || stored.Status != "complete" || stored.Asset != asset {
		return nil, ErrConflict
	}
	return s.readAsset(stored)
}

func imageRecordBelongsTo(row HouseImageRecord, r *Report) bool {
	return row.ReportID == r.ID && row.UserID == r.UserID && row.InputHash == r.InputHash && row.ConfirmationHash == r.ConfirmationHash
}
func validHouseImageOwner(r *Report) bool {
	return r != nil && validIdentifier(r.ID, 64) && r.UserID > 0 && r.ProtocolVersion == ProtocolHouseV1 && isImageHash(r.InputHash) && isImageHash(r.ConfirmationHash)
}
func validHouseImageTask(t HouseImageTask) bool {
	if !validIdentifier(t.ID, 80) || !validIdentifier(t.SchemeID, 64) || !validIdentifier(t.SchemeVersion, 64) || strings.TrimSpace(t.Prompt) == "" || len(t.Prompt) > 48000 || t.ID == t.ReferenceID {
		return false
	}
	if t.ReferenceID != "" && !validIdentifier(t.ReferenceID, 80) {
		return false
	}
	switch t.Kind {
	case "floor_plan":
		return t.Floor >= 1 && t.Floor <= 6
	case "massing", "exterior":
		return t.Floor == 0
	default:
		return false
	}
}
func imageDigest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func isImageHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && strings.ToLower(value) == value
}
func validateHouseImageBytes(data []byte) error {
	if len(data) == 0 || len(data) > houseImageMaxBytes {
		return ErrInvalidInput
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg") || cfg.Width != 2048 || cfg.Height != 1152 {
		return ErrInvalidInput
	}
	_, decoded, err := image.Decode(bytes.NewReader(data))
	if err != nil || decoded != format {
		return ErrInvalidInput
	}
	return nil
}
func (s *PersistentHouseImages) assetName(row HouseImageRecord) string {
	extension := ".png"
	if strings.HasSuffix(row.Asset.Asset, ".jpg") {
		extension = ".jpg"
	}
	return filepath.ToSlash(filepath.Join("house", imageDigest([]byte(row.ReportID)), row.RequestHash+extension))
}

func (s *PersistentHouseImages) receiptName(row HouseImageRecord) string {
	return filepath.ToSlash(filepath.Join("house", imageDigest([]byte(row.ReportID)), row.RequestHash+".receipt.json"))
}

// Internal file paths are derived from hashes, never from user-supplied paths.
// Check every component because a final-file check alone allows symlink parents.
func (s *PersistentHouseImages) privatePath(relative string, create bool) (string, error) {
	if filepath.IsAbs(relative) || filepath.Clean(relative) != relative || strings.Contains(relative, "\\") {
		return "", ErrInvalidInput
	}
	parts := strings.Split(relative, string(filepath.Separator))
	if len(parts) != 3 {
		return "", ErrInvalidInput
	}
	current := s.root
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", ErrInvalidInput
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) && create && i < len(parts)-1 {
			err = os.Mkdir(current, 0700)
			if err != nil && !errors.Is(err, os.ErrExist) {
				return "", imageIOFailure("create_directory", "", err)
			}
			info, err = os.Lstat(current)
		}
		if errors.Is(err, os.ErrNotExist) && i == len(parts)-1 {
			return current, nil
		}
		if err != nil {
			return "", imageIOFailure("stat_asset", "", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || i < len(parts)-1 && !info.IsDir() || i == len(parts)-1 && !info.Mode().IsRegular() {
			return "", ErrInvalidInput
		}
		if i < len(parts)-1 && info.Mode().Perm() != 0700 {
			return "", ErrInvalidInput
		}
	}
	return current, nil
}
func (s *PersistentHouseImages) saveReceipt(row HouseImageRecord, data []byte) error {
	name, err := s.privatePath(row.Asset.Asset, true)
	if err != nil {
		return err
	}
	if err := writePrivateImageFile(name, data); err != nil {
		return imageIOFailure("save_asset", row.ReportID, err)
	}
	receipt, err := json.Marshal(row)
	if err != nil {
		return ErrInvalidInput
	}
	// Publish the receipt only after both file and directory have been synced.
	receiptPath, err := s.privatePath(s.receiptName(row), false)
	if err != nil {
		return err
	}
	if err := writePrivateImageFile(receiptPath, receipt); err != nil {
		return imageIOFailure("save_receipt", row.ReportID, err)
	}
	return nil
}
func writePrivateImageFile(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".image-")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// Link publishes once; an interrupted/competing process cannot overwrite it.
	if err := os.Link(temp, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
func (s *PersistentHouseImages) loadReceipt(row HouseImageRecord) (HouseImageRecord, error) {
	name, err := s.privatePath(s.receiptName(row), false)
	if err != nil {
		return HouseImageRecord{}, ErrImageUncertain
	}
	raw, err := readPrivateImageFile(name, 128<<10)
	if err != nil {
		return HouseImageRecord{}, ErrImageUncertain
	}
	var recovered HouseImageRecord
	if json.Unmarshal(raw, &recovered) != nil || recovered.RequestHash != row.RequestHash || recovered.Status != "complete" || recovered.ReportID != row.ReportID || recovered.UserID != row.UserID || recovered.InputHash != row.InputHash || recovered.ConfirmationHash != row.ConfirmationHash || recovered.Task != row.Task || recovered.Identity != row.Identity || recovered.ReferenceHash != row.ReferenceHash {
		return HouseImageRecord{}, ErrImageUncertain
	}
	if _, err := s.readAsset(recovered); err != nil {
		return HouseImageRecord{}, ErrImageUncertain
	}
	return recovered, nil
}
func (s *PersistentHouseImages) readAsset(row HouseImageRecord) ([]byte, error) {
	a := row.Asset
	if a.Asset != s.assetName(row) || !isImageHash(a.SHA256) || a.Width != 2048 || a.Height != 1152 || a.ID != row.Task.ID || a.SchemeID != row.Task.SchemeID || a.SchemeVersion != row.Task.SchemeVersion || a.Kind != row.Task.Kind || a.Floor != row.Task.Floor || a.Model != row.Identity.Model || a.ReferenceID != row.Task.ReferenceID {
		return nil, ErrInvalidInput
	}
	name, err := s.privatePath(a.Asset, false)
	if err != nil {
		return nil, err
	}
	data, err := readPrivateImageFile(name, houseImageMaxBytes)
	if err != nil {
		return nil, imageIOFailure("read_asset", row.ReportID, err)
	}
	if imageDigest(data) != a.SHA256 || validateHouseImageBytes(data) != nil {
		return nil, ErrInvalidInput
	}
	return data, nil
}
func readPrivateImageFile(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() <= 0 || info.Size() > limit {
		return nil, ErrInvalidInput
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, opened) {
		return nil, ErrInvalidInput
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, ErrInvalidInput
	}
	return data, nil
}
func imageIOFailure(operation, reportID string, _ error) error {
	log.Printf("[report-image] error: operation=%s report_id=%s", operation, reportID)
	return errors.New("report: private image storage unavailable")
}

var _ HouseImageService = (*PersistentHouseImages)(nil)
