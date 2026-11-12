package function

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	stddraw "image/draw"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"service/component"
	"service/config"

	cos "github.com/tencentyun/cos-go-sdk-v5"
)

type ImageVariantSpec struct {
	Name     string
	MaxWidth int
	Quality  int
}

type ImageVariantResult struct {
	SourceWidth  int
	SourceHeight int
	URLs         map[string]string
}

var variantHTTPClient = &http.Client{
	Timeout: 15 * time.Second,
}

const (
	maxRemoteVariantSourceBytes = 50 << 20
	maxVariantSourcePixels      = 100_000_000
	variantEncoderVersion       = "jpeg-v1"
	variantKeyMetadataHeader    = "x-cos-meta-jdlg-variant-key"
)

func GenerateRemoteImageVariants(ctx context.Context, cfg *config.Config, client *cos.Client, sourceURL string, namespace string, specs []ImageVariantSpec) (*ImageVariantResult, error) {
	cleanURL := strings.TrimSpace(sourceURL)
	if cleanURL == "" {
		return nil, fmt.Errorf("source image url is empty")
	}
	if cfg == nil {
		cfg = config.Get()
	}
	if client == nil {
		client = component.GetCOSClient()
	}
	if cfg == nil || client == nil {
		return nil, fmt.Errorf("COS client is not initialized")
	}

	srcImage, sourceDigest, width, height, err := downloadImage(ctx, cleanURL)
	if err != nil {
		return nil, err
	}

	return generateImageVariants(ctx, cfg, client, sourceDigest, srcImage, width, height, namespace, specs)
}

func GenerateImageVariantsFromBytes(ctx context.Context, cfg *config.Config, client *cos.Client, sourceKey string, imageBytes []byte, namespace string, specs []ImageVariantSpec) (*ImageVariantResult, error) {
	cleanKey := strings.TrimSpace(sourceKey)
	if cleanKey == "" {
		return nil, fmt.Errorf("source key is empty")
	}
	if len(imageBytes) == 0 {
		return nil, fmt.Errorf("image bytes are empty")
	}
	if cfg == nil {
		cfg = config.Get()
	}
	if client == nil {
		client = component.GetCOSClient()
	}
	if cfg == nil || client == nil {
		return nil, fmt.Errorf("COS client is not initialized")
	}

	img, width, height, err := decodeVariantSource(imageBytes)
	if err != nil {
		return nil, err
	}
	sourceDigest := sha256.Sum256(imageBytes)
	return generateImageVariants(ctx, cfg, client, sourceDigest, img, width, height, namespace, specs)
}

func generateImageVariants(ctx context.Context, cfg *config.Config, client *cos.Client, sourceDigest [sha256.Size]byte, srcImage image.Image, width int, height int, namespace string, specs []ImageVariantSpec) (*ImageVariantResult, error) {
	if srcImage == nil || width <= 0 || height <= 0 {
		return nil, fmt.Errorf("source image is invalid")
	}
	cleanNamespace, err := cleanVariantNamespace(namespace)
	if err != nil {
		return nil, err
	}

	result := &ImageVariantResult{
		SourceWidth:  width,
		SourceHeight: height,
		URLs:         make(map[string]string, len(specs)),
	}

	for _, spec := range specs {
		spec.Name = strings.TrimSpace(spec.Name)
		if spec.Name == "" || spec.MaxWidth <= 0 {
			continue
		}
		if !isSafeVariantName(spec.Name) {
			return nil, fmt.Errorf("variant name %q is invalid", spec.Name)
		}
		spec.Quality = normalizeVariantQuality(spec.Quality)
		hash := buildVariantHash(sourceDigest, spec)
		objectKey := path.Join("derived_images", cleanNamespace, fmt.Sprintf("%s_%s.jpg", hash, spec.Name))
		if url, ok := reusableImageVariantURL(ctx, client, cfg, objectKey, hash); ok {
			result.URLs[spec.Name] = url
			continue
		}
		imageBytes, err := buildJPEGVariant(srcImage, width, height, spec.MaxWidth, spec.Quality)
		if err != nil {
			return nil, fmt.Errorf("build variant %s failed: %w", spec.Name, err)
		}
		url, err := uploadImageVariant(ctx, client, cfg, objectKey, hash, imageBytes)
		if err != nil {
			return nil, fmt.Errorf("upload variant %s failed: %w", spec.Name, err)
		}
		result.URLs[spec.Name] = url
	}

	return result, nil
}

func buildVariantHash(sourceDigest [sha256.Size]byte, spec ImageVariantSpec) string {
	digest := sha256.New()
	digest.Write([]byte(variantEncoderVersion))
	digest.Write([]byte{0})
	digest.Write(sourceDigest[:])
	digest.Write([]byte{0})
	digest.Write([]byte(strings.TrimSpace(spec.Name)))
	digest.Write([]byte{0})
	digest.Write([]byte(strconv.Itoa(spec.MaxWidth)))
	digest.Write([]byte{0})
	digest.Write([]byte(strconv.Itoa(normalizeVariantQuality(spec.Quality))))
	sum := digest.Sum(nil)
	return hex.EncodeToString(sum[:20])
}

func downloadImage(ctx context.Context, sourceURL string) (image.Image, [sha256.Size]byte, int, int, error) {
	var emptyDigest [sha256.Size]byte
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, emptyDigest, 0, 0, err
	}
	req.Header.Set("Accept", "image/*")
	req.Header.Set("User-Agent", "jdlg-image-variants/1.0")

	resp, err := variantHTTPClient.Do(req)
	if err != nil {
		return nil, emptyDigest, 0, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, emptyDigest, 0, 0, fmt.Errorf("download image failed with status %d", resp.StatusCode)
	}
	imageBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxRemoteVariantSourceBytes+1))
	if err != nil {
		return nil, emptyDigest, 0, 0, err
	}
	if len(imageBytes) > maxRemoteVariantSourceBytes {
		return nil, emptyDigest, 0, 0, fmt.Errorf("source image exceeds %d bytes", maxRemoteVariantSourceBytes)
	}
	img, width, height, err := decodeVariantSource(imageBytes)
	if err != nil {
		return nil, emptyDigest, 0, 0, err
	}
	return img, sha256.Sum256(imageBytes), width, height, nil
}

func decodeVariantSource(imageBytes []byte) (image.Image, int, int, error) {
	info, _, err := image.DecodeConfig(bytes.NewReader(imageBytes))
	if err != nil {
		return nil, 0, 0, err
	}
	if info.Width <= 0 || info.Height <= 0 || info.Width > maxVariantSourcePixels/info.Height {
		return nil, 0, 0, fmt.Errorf("source image dimensions are invalid or exceed %d pixels", maxVariantSourcePixels)
	}
	img, _, err := image.Decode(bytes.NewReader(imageBytes))
	if err != nil {
		return nil, 0, 0, err
	}
	return img, info.Width, info.Height, nil
}

func cleanVariantNamespace(namespace string) (string, error) {
	clean := strings.Trim(strings.TrimSpace(namespace), "/")
	if clean == "" || clean != path.Clean(clean) || strings.Contains(clean, "\\") || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("variant namespace is invalid")
	}
	return clean, nil
}

func isSafeVariantName(name string) bool {
	for _, char := range name {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '_' {
			continue
		}
		return false
	}
	return name != ""
}

func normalizeVariantQuality(quality int) int {
	if quality <= 0 || quality > 100 {
		return 82
	}
	return quality
}

func reusableImageVariantURL(ctx context.Context, client *cos.Client, cfg *config.Config, objectKey string, hash string) (string, bool) {
	fullKey := buildObjectKey(cfg, objectKey)
	resp, err := client.Object.Head(ctx, fullKey, nil)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil || resp == nil {
		return "", false
	}
	contentLength, err := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	if err != nil || contentLength <= 0 || !strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "image/jpeg") {
		return "", false
	}
	if resp.Header.Get(variantKeyMetadataHeader) != hash {
		return "", false
	}
	return component.BuildObjectURL(cfg, fullKey), true
}

func uploadImageVariant(ctx context.Context, client *cos.Client, cfg *config.Config, objectKey string, hash string, imageBytes []byte) (string, error) {
	fullKey := buildObjectKey(cfg, objectKey)
	metadata := make(http.Header)
	metadata.Set(variantKeyMetadataHeader, hash)
	_, err := client.Object.Put(ctx, fullKey, bytes.NewReader(imageBytes), &cos.ObjectPutOptions{
		ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{
			ContentType: "image/jpeg",
			XCosMetaXXX: &metadata,
		},
	})
	if err != nil {
		return "", err
	}
	return component.BuildObjectURL(cfg, fullKey), nil
}

func buildJPEGVariant(src image.Image, srcWidth int, srcHeight int, maxWidth int, quality int) ([]byte, error) {
	targetWidth := srcWidth
	targetHeight := srcHeight
	if srcWidth > maxWidth {
		targetWidth = maxWidth
		targetHeight = int(float64(srcHeight) * float64(maxWidth) / float64(srcWidth))
	}
	if targetWidth <= 0 || targetHeight <= 0 {
		return nil, fmt.Errorf("invalid target size %dx%d", targetWidth, targetHeight)
	}
	quality = normalizeVariantQuality(quality)

	dst := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	stddraw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.White}, image.Point{}, stddraw.Src)
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
