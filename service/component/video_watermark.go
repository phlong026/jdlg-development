package component

import (
	"bytes"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// 与 AI 图片水印一致的文字，仅文字、无图标（见 component/watermark.go 的 defaultWatermarkText）
const videoWatermarkText = "ArchLight 3.0 Ai设计"

// findWatermarkFontPath 查找与图片水印相同的字体路径，供 ffmpeg 显示中文
func findWatermarkFontPath() string {
	bases := make([]string, 0, 8)
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		bases = append(bases, filepath.Join(dir, "fonts", "default"), filepath.Join(dir, "default"))
	}
	bases = append(bases, "fonts/default", "default", "component/fonts/default")
	exts := []string{".ttf", ".otf"}
	for _, base := range bases {
		for _, ext := range exts {
			p := base + ext
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

// AddVideoWatermark 对视频叠加文字水印（与 AI 图片手动加的水印同文、无图标）。
// 使用 ffmpeg drawtext 右下角白字。若未安装 ffmpeg 或执行失败则返回原始数据与 error，调用方可用原视频上传。
func AddVideoWatermark(videoData []byte) ([]byte, error) {
	if len(videoData) == 0 {
		return videoData, nil
	}
	dir := os.TempDir()
	inFile, err := os.CreateTemp(dir, "av_in_")
	if err != nil {
		return nil, err
	}
	inPath := inFile.Name()
	defer os.Remove(inPath)
	if _, err := io.Copy(inFile, bytes.NewReader(videoData)); err != nil {
		inFile.Close()
		return nil, err
	}
	if err := inFile.Close(); err != nil {
		return nil, err
	}
	outFile, err := os.CreateTemp(dir, "av_out_")
	if err != nil {
		return nil, err
	}
	outPath := outFile.Name()
	outFile.Close()
	defer os.Remove(outPath)

	// drawtext: 右下角白字，与图片水印一致（无 logo）。若有字体则指定 fontfile 以正确显示中文
	filter := "drawtext=text='" + videoWatermarkText + "':fontcolor=white:fontsize=24:x=w-tw-20:y=h-th-20"
	if fontPath := findWatermarkFontPath(); fontPath != "" {
		// ffmpeg 的 fontfile 路径需转义反斜杠或使用正斜杠
		fontPath = strings.ReplaceAll(fontPath, "\\", "/")
		filter = "drawtext=fontfile='" + fontPath + "':text='" + videoWatermarkText + "':fontcolor=white:fontsize=24:x=w-tw-20:y=h-th-20"
	}
	cmd := exec.Command("ffmpeg", "-y", "-i", inPath, "-vf", filter, "-c:a", "copy", outPath)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		log.Printf("[video_watermark] ffmpeg 执行失败（请安装 ffmpeg）: %v", err)
		return nil, err
	}
	outData, err := os.ReadFile(outPath)
	if err != nil {
		return nil, err
	}
	return outData, nil
}

// ConcatVideos 使用 ffmpeg concat demuxer 将多段视频按顺序拼接为一段（不重编码，-c copy）。
// 若只有一段则直接返回该段；若任一段为空或 ffmpeg 失败则返回 nil, error。
func ConcatVideos(segments [][]byte) ([]byte, error) {
	if len(segments) == 0 {
		return nil, nil
	}
	if len(segments) == 1 {
		return segments[0], nil
	}
	dir := os.TempDir()
	var paths []string
	defer func() {
		for _, p := range paths {
			os.Remove(p)
		}
	}()
	for i, seg := range segments {
		if len(seg) == 0 {
			return nil, nil
		}
		f, err := os.CreateTemp(dir, "concat_seg_")
		if err != nil {
			return nil, err
		}
		p := f.Name()
		paths = append(paths, p)
		if _, err := io.Copy(f, bytes.NewReader(seg)); err != nil {
			f.Close()
			return nil, err
		}
		if err := f.Close(); err != nil {
			return nil, err
		}
		_ = i
	}
	// concat list file: file 'path1'\nfile 'path2'...
	// paths may contain single quotes - escape as '\''
	listContent := ""
	for _, p := range paths {
		escaped := strings.ReplaceAll(p, "'", "'\\''")
		listContent += "file '" + escaped + "'\n"
	}
	listFile, err := os.CreateTemp(dir, "concat_list_")
	if err != nil {
		return nil, err
	}
	listPath := listFile.Name()
	defer os.Remove(listPath)
	if _, err := listFile.WriteString(listContent); err != nil {
		listFile.Close()
		return nil, err
	}
	if err := listFile.Close(); err != nil {
		return nil, err
	}
	outFile, err := os.CreateTemp(dir, "concat_out_")
	if err != nil {
		return nil, err
	}
	outPath := outFile.Name()
	outFile.Close()
	defer os.Remove(outPath)

	cmd := exec.Command("ffmpeg", "-y", "-f", "concat", "-safe", "0", "-i", listPath, "-c", "copy", outPath)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		log.Printf("[video_concat] ffmpeg concat 失败: %v", err)
		return nil, err
	}
	outData, err := os.ReadFile(outPath)
	if err != nil {
		return nil, err
	}
	return outData, nil
}
