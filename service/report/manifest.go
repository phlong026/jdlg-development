package report

import (
	"bytes"
	"encoding/json"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type packManifest struct {
	Version string         `json:"version"`
	Packs   []ReviewedPack `json:"packs"`
}

type FilePackCatalog struct {
	packs          []ReviewedPack
	assetsVerified bool
}

func NewFilePackCatalog(manifestPath string) (*FilePackCatalog, error) {
	if !filepath.IsAbs(manifestPath) {
		return nil, ErrPackUnavailable
	}
	file, err := os.Open(manifestPath)
	if err != nil {
		return nil, ErrPackUnavailable
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 4<<20+1))
	if err != nil || len(data) > 4<<20 {
		return nil, ErrPackUnavailable
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest packManifest
	if decoder.Decode(&manifest) != nil || manifest.Version != "reviewed-pack-v1" || len(manifest.Packs) == 0 {
		return nil, ErrPackUnavailable
	}
	identities := make(map[string]struct{}, len(manifest.Packs))
	for index := range manifest.Packs {
		if err := ValidateReviewedPack(manifest.Packs[index]); err != nil {
			return nil, err
		}
		identity := manifest.Packs[index].ID + "\x00" + manifest.Packs[index].Version
		if _, duplicate := identities[identity]; duplicate {
			return nil, ErrPackUnavailable
		}
		identities[identity] = struct{}{}
	}
	sort.Slice(manifest.Packs, func(i, j int) bool {
		if manifest.Packs[i].ID == manifest.Packs[j].ID {
			return manifest.Packs[i].Version > manifest.Packs[j].Version
		}
		return manifest.Packs[i].ID < manifest.Packs[j].ID
	})
	return &FilePackCatalog{packs: manifest.Packs}, nil
}

func (c *FilePackCatalog) Match(input Input) (ReviewedPack, error) {
	if c == nil || !c.assetsVerified {
		return ReviewedPack{}, ErrPackUnavailable
	}
	normalized, err := NormalizeAndValidateInput(input)
	if err != nil {
		return ReviewedPack{}, err
	}
	for _, pack := range c.packs {
		if !packSupportsInput(pack, normalized) {
			continue
		}
		for _, style := range pack.Styles {
			if strings.EqualFold(strings.TrimSpace(style), normalized.Preference.Style) {
				return pack, nil
			}
		}
	}
	return ReviewedPack{}, ErrPackUnavailable
}

func (c *FilePackCatalog) Get(id, version string) (ReviewedPack, error) {
	if c == nil || !c.assetsVerified {
		return ReviewedPack{}, ErrPackUnavailable
	}
	for _, pack := range c.packs {
		if pack.ID == id && pack.Version == version {
			return pack, nil
		}
	}
	return ReviewedPack{}, ErrPackUnavailable
}

// VerifyAssetRoot is a startup gate for paid reports. It canonicalizes every
// reviewed asset and rejects files that the renderer could not safely read.
func (c *FilePackCatalog) VerifyAssetRoot(root string) error {
	if c == nil || !filepath.IsAbs(root) {
		return ErrPackUnavailable
	}
	canonicalRoot, err := filepath.EvalSymlinks(filepath.Clean(root))
	if err != nil {
		return ErrPackUnavailable
	}
	rootInfo, err := os.Stat(canonicalRoot)
	if err != nil || !rootInfo.IsDir() {
		return ErrPackUnavailable
	}
	for packIndex := range c.packs {
		for assetID, assetPath := range c.packs[packIndex].Assets {
			canonicalAsset, err := filepath.EvalSymlinks(filepath.Clean(assetPath))
			if err != nil || !assetWithinRoot(canonicalRoot, canonicalAsset) {
				return ErrPackUnavailable
			}
			extension := strings.ToLower(filepath.Ext(canonicalAsset))
			if extension != ".png" && extension != ".jpg" && extension != ".jpeg" {
				return ErrPackUnavailable
			}
			info, err := os.Stat(canonicalAsset)
			if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 12<<20 {
				return ErrPackUnavailable
			}
			file, err := os.Open(canonicalAsset)
			if err != nil {
				return ErrPackUnavailable
			}
			config, format, decodeErr := image.DecodeConfig(io.LimitReader(file, (12<<20)+1))
			formatMatches := extension == ".png" && format == "png" ||
				(extension == ".jpg" || extension == ".jpeg") && format == "jpeg"
			if decodeErr != nil || !formatMatches || config.Width < 1 || config.Height < 1 || config.Width > 6000 || config.Height > 6000 {
				_ = file.Close()
				return ErrPackUnavailable
			}
			if _, err := file.Seek(0, io.SeekStart); err != nil {
				_ = file.Close()
				return ErrPackUnavailable
			}
			_, decodedFormat, fullDecodeErr := image.Decode(io.LimitReader(file, (12<<20)+1))
			closeErr := file.Close()
			if fullDecodeErr != nil || closeErr != nil || decodedFormat != format {
				return ErrPackUnavailable
			}
			c.packs[packIndex].Assets[assetID] = canonicalAsset
		}
	}
	c.assetsVerified = true
	return nil
}

func assetWithinRoot(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." || relative == ".." {
		return false
	}
	return !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func ValidateReviewedPack(pack ReviewedPack) error {
	if !validIdentifier(pack.ID, 64) || !validIdentifier(pack.Version, 64) || pack.ReviewStatus != "approved" || len(pack.Styles) == 0 {
		return ErrPackUnavailable
	}
	for _, style := range pack.Styles {
		if !allowedStyles[style] {
			return ErrPackUnavailable
		}
	}
	if !boundedNumber(pack.MinWidthM, 1, 100) || !boundedNumber(pack.MaxWidthM, pack.MinWidthM, 100) || !boundedNumber(pack.MinDepthM, 1, 150) || !boundedNumber(pack.MaxDepthM, pack.MinDepthM, 150) || pack.Floors < 1 || pack.Floors > 3 || !allowedBudgetScopes[pack.BudgetScope] {
		return ErrPackUnavailable
	}
	if len(pack.Assets) < 6 || len(pack.Schemes) != 3 {
		return ErrPackUnavailable
	}
	for id, path := range pack.Assets {
		if !validIdentifier(id, 80) || !filepath.IsAbs(path) || strings.Contains(path, "://") {
			return ErrPackUnavailable
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return ErrPackUnavailable
		}
	}
	for index, scheme := range pack.Schemes {
		expectedID := string(rune('A' + index))
		if scheme.ID != expectedID || !boundedManifestText(scheme.Name, 1, 8) || !boundedManifestText(scheme.Tagline, 1, 36) || !boundedManifestText(scheme.Tradeoff, 1, 30) || !boundedManifestText(scheme.Gain, 1, 30) || !boundedManifestText(scheme.Facade, 1, 70) || !boundedManifestText(scheme.FacadeCaution, 1, 70) || scheme.Bedrooms < 1 || scheme.Bedrooms > 12 || len(scheme.PriorityFit) != 3 || len(scheme.CostSensitive) < 1 || len(scheme.CostSensitive) > 2 || len(scheme.Plans) != pack.Floors {
			return ErrPackUnavailable
		}
		seenPriorities := make(map[string]struct{}, 3)
		for _, priority := range scheme.PriorityFit {
			if !allowedPriorities[priority] {
				return ErrPackUnavailable
			}
			if _, duplicate := seenPriorities[priority]; duplicate {
				return ErrPackUnavailable
			}
			seenPriorities[priority] = struct{}{}
		}
		for _, item := range scheme.CostSensitive {
			if !boundedManifestText(item, 1, 12) {
				return ErrPackUnavailable
			}
		}
		if _, exists := pack.Assets[scheme.FacadeAssetID]; !exists {
			return ErrPackUnavailable
		}
		if scheme.VolumeAssetID == scheme.FacadeAssetID {
			return ErrPackUnavailable
		}
		if _, exists := pack.Assets[scheme.VolumeAssetID]; !exists {
			return ErrPackUnavailable
		}
		for floorIndex, plan := range scheme.Plans {
			if plan.Floor != floorIndex+1 || !boundedManifestText(plan.Name, 1, 12) || !boundedManifestText(plan.Summary, 1, 50) {
				return ErrPackUnavailable
			}
			if _, exists := pack.Assets[plan.AssetID]; !exists {
				return ErrPackUnavailable
			}
		}
	}
	if len(pack.BudgetTemplate) != 5 {
		return ErrPackUnavailable
	}
	total := 0.0
	for _, row := range pack.BudgetTemplate {
		if !boundedManifestText(row.Name, 1, 12) || math.IsNaN(row.Percent) || math.IsInf(row.Percent, 0) || row.Percent <= 0 || row.Percent > 100 {
			return ErrPackUnavailable
		}
		total += row.Percent
	}
	if math.Abs(total-100) > 0.00001 {
		return ErrPackUnavailable
	}
	return nil
}

func packSupportsInput(pack ReviewedPack, input Input) bool {
	if pack.Floors != input.Space.Floors || pack.BudgetScope != input.Budget.Scope ||
		input.Site.WidthM < pack.MinWidthM || input.Site.WidthM > pack.MaxWidthM ||
		input.Site.DepthM < pack.MinDepthM || input.Site.DepthM > pack.MaxDepthM {
		return false
	}
	for _, scheme := range pack.Schemes {
		if scheme.Bedrooms != input.Space.Bedrooms || input.Space.FirstFloorElderRoom && !scheme.FirstFloorElderRoom {
			return false
		}
	}
	return true
}

func boundedManifestText(value string, minimum, maximum int) bool {
	if !boundedText(value, minimum, maximum) || strings.ContainsAny(value, "\r\n") {
		return false
	}
	for _, char := range value {
		if char < 0x20 || char == 0x7f {
			return false
		}
	}
	return true
}
