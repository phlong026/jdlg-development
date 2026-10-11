package function

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	stddraw "image/draw"
	"image/jpeg"
	_ "image/png"
	"net/http"
	"path"
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

	srcImage, width, height, err := downloadImage(cleanURL)
	if err != nil {
		return nil, err
	}

	return generateImageVariants(ctx, cfg, client, cleanURL, srcImage, width, height, namespace, specs)
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

	img, _, err := image.Decode(bytes.NewReader(imageBytes))
	if err != nil {
		return nil, err
	}
	bounds := img.Bounds()
	return generateImageVariants(ctx, cfg, client, cleanKey, img, bounds.Dx(), bounds.Dy(), namespace, specs)
}

func generateImageVariants(ctx context.Context, cfg *config.Config, client *cos.Client, sourceIdentity string, srcImage image.Image, width int, height int, namespace string, specs []ImageVariantSpec) (*ImageVariantResult, error) {
	cleanIdentity := strings.TrimSpace(sourceIdentity)
	if cleanIdentity == "" {
		return nil, fmt.Errorf("source identity is empty")
	}
	if srcImage == nil || width <= 0 || height <= 0 {
		return nil, fmt.Errorf("source image is invalid")
	}

	result := &ImageVariantResult{
		SourceWidth:  width,
		SourceHeight: height,
		URLs:         make(map[string]string, len(specs)),
	}

	hash := buildVariantHash(cleanIdentity)
	for _, spec := range specs {
		if strings.TrimSpace(spec.Name) == "" || spec.MaxWidth <= 0 {
			continue
		}
		imageBytes, err := buildJPEGVariant(srcImage, width, height, spec.MaxWidth, spec.Quality)
		if err != nil {
			return nil, fmt.Errorf("build variant %s failed: %w", spec.Name, err)
		}
		objectKey := path.Join("derived_images", strings.Trim(strings.TrimSpace(namespace), "/"), fmt.Sprintf("%s_%s.jpg", hash, spec.Name))
		url, err := UploadBytes(ctx, client, cfg, objectKey, imageBytes, "image/jpeg")
		if err != nil {
			return nil, fmt.Errorf("upload variant %s failed: %w", spec.Name, err)
		}
		result.URLs[spec.Name] = url
	}

	return result, nil
}

func buildVariantHash(sourceURL string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(sourceURL)))
	return hex.EncodeToString(sum[:])
}

func downloadImage(sourceURL string) (image.Image, int, int, error) {
	req, err := http.NewRequest(http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, 0, 0, err
	}
	req.Header.Set("Accept", "image/*")
	req.Header.Set("User-Agent", "jdlg-image-variants/1.0")

	resp, err := variantHTTPClient.Do(req)
	if err != nil {
		return nil, 0, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, 0, 0, fmt.Errorf("download image failed with status %d", resp.StatusCode)
	}

	img, _, err := image.Decode(resp.Body)
	if err != nil {
		return nil, 0, 0, err
	}
	bounds := img.Bounds()
	return img, bounds.Dx(), bounds.Dy(), nil
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
	if quality <= 0 || quality > 100 {
		quality = 82
	}

	dst := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	stddraw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.White}, image.Point{}, stddraw.Src)
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
