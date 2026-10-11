package component

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	defaultWatermarkText = "ArchLight 3.0 Ai设计"
	watermarkFontSize    = 28  // 字号（pt）
	watermarkPadding     = 16  // 右下角边距
	watermarkShiftRight  = 20  // 整体再靠右一点（像素）
	watermarkLogoGap     = 5   // logo 与文字间距
	watermarkLogoScale   = 1.4 // logo 高度为文字行高的倍数
	shadowOffset         = 1
)

var (
	watermarkFace     font.Face
	watermarkFaceOnce sync.Once
	watermarkLogo     image.Image
	watermarkLogoOnce sync.Once
)

// loadWatermarkFace 尝试加载支持中文的 TTF，失败则返回 nil（将用 basicfont 兜底）
func loadWatermarkFace() font.Face {
	var face font.Face
	watermarkFaceOnce.Do(func() {
		// 支持 TTF 与 OTF（思源黑体等多为 OTF）
		// 优先从可执行文件同目录查找，便于打包部署（exe 与 fonts 文件夹放一起即可）
		bases := make([]string, 0, 8)
		if exe, err := os.Executable(); err == nil {
			dir := filepath.Dir(exe)
			bases = append(bases, filepath.Join(dir, "fonts", "default"), filepath.Join(dir, "default"))
		}
		bases = append(bases, "fonts/default", "default", "component/fonts/default")
		exts := []string{".ttf", ".otf"}
		var fontData []byte
		var loadedPath string
		for _, base := range bases {
			for _, ext := range exts {
				p := base + ext
				fontData, _ = os.ReadFile(p)
				if len(fontData) > 0 {
					loadedPath = p
					break
				}
			}
			if len(fontData) > 0 {
				break
			}
		}
		if len(fontData) == 0 {
			log.Printf("[watermark] 未找到 fonts/default.ttf 或 default.otf，中文水印可能乱码，请放置支持中文的 TTF/OTF")
			return
		}
		parsed, err := opentype.Parse(fontData)
		if err != nil {
			log.Printf("[watermark] 解析字体失败 %s: %v", loadedPath, err)
			return
		}
		face, err = opentype.NewFace(parsed, &opentype.FaceOptions{
			Size:    watermarkFontSize,
			DPI:     72,
			Hinting: font.HintingFull,
		})
		if err != nil {
			log.Printf("[watermark] NewFace 失败: %v", err)
			return
		}
		watermarkFace = face
		log.Printf("[watermark] 已加载字体 %s，字号 %d", loadedPath, watermarkFontSize)
	})
	return watermarkFace
}

// loadWatermarkLogo 从 assets/logo.png 加载水印 logo（打包后与可执行文件同目录的 assets 下）
func loadWatermarkLogo() image.Image {
	watermarkLogoOnce.Do(func() {
		bases := make([]string, 0, 6)
		if exe, err := os.Executable(); err == nil {
			dir := filepath.Dir(exe)
			bases = append(bases, filepath.Join(dir, "assets", "logo.png"))
		}
		bases = append(bases, "assets/logo.png", "component/assets/logo.png")
		var data []byte
		var path string
		for _, p := range bases {
			data, _ = os.ReadFile(p)
			if len(data) > 0 {
				path = p
				break
			}
		}
		if len(data) == 0 {
			return
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			log.Printf("[watermark] 解析 logo 失败 %s: %v", path, err)
			return
		}
		watermarkLogo = img
		log.Printf("[watermark] 已加载 logo %s", path)
	})
	return watermarkLogo
}

// AddWatermark 在图片右下角绘制水印（logo + 白字+黑阴影）；logo 取自打包后 assets/logo.png
func AddWatermark(imgData []byte, text string) ([]byte, error) {
	if text == "" {
		text = defaultWatermarkText
	}
	img, fmtName, err := decodeImageForWatermark(imgData)
	if err != nil {
		return nil, err
	}
	bounds := img.Bounds()
	dst := image.NewRGBA(bounds)
	draw.Draw(dst, bounds, img, bounds.Min, draw.Src)

	face := loadWatermarkFace()
	if face == nil {
		face = basicfont.Face7x13
	}

	// 测量文字宽度，用于右下角对齐
	adv := font.MeasureString(face, text)
	textW := adv.Ceil()
	metrics := face.Metrics()
	ascent := metrics.Ascent.Ceil()
	descent := metrics.Descent.Ceil()
	lineH := ascent + descent

	// 右下角：整体 [logo][gap][text]，右边界对齐；整体稍微靠右一点
	textX := bounds.Dx() - watermarkPadding - textW + watermarkShiftRight
	rectY := bounds.Dy() - watermarkPadding - lineH
	logoHeight := lineH
	logoImg := loadWatermarkLogo()
	if logoImg != nil {
		logoBounds := logoImg.Bounds()
		lw, lh := logoBounds.Dx(), logoBounds.Dy()
		if lh > 0 {
			logoHeight = int(float64(lineH)*watermarkLogoScale + 0.5)
			if logoHeight < lineH+4 {
				logoHeight = lineH + 4
			}
			scaleH := float64(logoHeight) / float64(lh)
			logoW := int(float64(lw)*scaleH + 0.5)
			if logoW > 0 && logoW < bounds.Dx()/2 {
				logoX := textX - watermarkLogoGap - logoW
				// logo 与文字横向（垂直方向）居中对齐：logo 中心 Y = 文字行中心 Y
				textLineCenterY := rectY + lineH/2
				logoRectY := textLineCenterY - logoHeight/2
				if logoRectY < 0 {
					logoRectY = 0
				}
				if logoRectY+logoHeight > bounds.Dy() {
					logoRectY = bounds.Dy() - logoHeight
				}
				logoRect := image.Rect(logoX, logoRectY, logoX+logoW, logoRectY+logoHeight)
				scaledLogo := image.NewRGBA(logoRect)
				draw.CatmullRom.Scale(scaledLogo, scaledLogo.Bounds(), logoImg, logoBounds, draw.Over, nil)
				draw.Draw(dst, logoRect, scaledLogo, logoRect.Min, draw.Over)
			}
		}
	}
	if textX < 0 {
		textX = 0
	}
	if rectY < 0 {
		rectY = 0
	}
	baselineY := rectY + ascent

	// 先画黑色阴影（偏移 1 像素）
	drawStringWatermark(dst, face, fixed.P(textX+shadowOffset, baselineY+shadowOffset), text, color.Black)
	// 再画白色文字
	drawStringWatermark(dst, face, fixed.P(textX, baselineY), text, color.White)

	var out bytes.Buffer
	if strings.ToLower(fmtName) == "png" {
		err = png.Encode(&out, dst)
	} else {
		err = jpeg.Encode(&out, dst, &jpeg.Options{Quality: 100})
	}
	if err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func decodeImageForWatermark(data []byte) (image.Image, string, error) {
	contentType := detectImageTypeForWatermark(data)
	r := bytes.NewReader(data)
	var img image.Image
	var err error
	switch contentType {
	case "png":
		img, err = png.Decode(r)
		return img, "png", err
	default:
		img, err = jpeg.Decode(r)
		return img, "jpeg", err
	}
}

func detectImageTypeForWatermark(data []byte) string {
	if len(data) < 12 {
		return "jpeg"
	}
	if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4e {
		return "png"
	}
	if data[0] == 0xff && data[1] == 0xd8 {
		return "jpeg"
	}
	return "jpeg"
}

func drawStringWatermark(img *image.RGBA, face font.Face, p fixed.Point26_6, s string, c color.Color) {
	drawer := font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(c),
		Face: face,
		Dot:  p,
	}
	drawer.DrawString(s)
}

// applySharpening 应用轻微锐化效果，使用拉普拉斯锐化核
// 强度设置为 0.3，提供轻微的锐化效果，不会过度
func applySharpening(img *image.RGBA) *image.RGBA {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	sharpened := image.NewRGBA(bounds)

	// 锐化核（拉普拉斯算子，轻微强度）
	// 中心值 5.0，周围 -1.0，强度因子 0.3
	strength := 0.3
	kernel := [3][3]float64{
		{0, -1 * strength, 0},
		{-1 * strength, 1 + 4*strength, -1 * strength},
		{0, -1 * strength, 0},
	}

	for y := 1; y < height-1; y++ {
		for x := 1; x < width-1; x++ {
			var r, g, b, a float64

			// 应用卷积核
			for ky := -1; ky <= 1; ky++ {
				for kx := -1; kx <= 1; kx++ {
					px := x + kx
					py := y + ky
					c := img.RGBAAt(px, py)
					weight := kernel[ky+1][kx+1]

					r += float64(c.R) * weight
					g += float64(c.G) * weight
					b += float64(c.B) * weight
					a += float64(c.A) * weight
				}
			}

			// 限制值在 0-255 范围内
			r = clamp(r, 0, 255)
			g = clamp(g, 0, 255)
			b = clamp(b, 0, 255)
			a = clamp(a, 0, 255)

			sharpened.SetRGBA(x, y, color.RGBA{
				R: uint8(r),
				G: uint8(g),
				B: uint8(b),
				A: uint8(a),
			})
		}
	}

	// 边缘像素直接复制，不进行锐化
	for y := 0; y < height; y++ {
		if y == 0 || y == height-1 {
			for x := 0; x < width; x++ {
				sharpened.Set(x, y, img.At(x, y))
			}
		} else {
			sharpened.Set(0, y, img.At(0, y))
			sharpened.Set(width-1, y, img.At(width-1, y))
		}
	}

	return sharpened
}

// clamp 限制值在指定范围内
func clamp(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
