// Command render_report renders an approved report snapshot locally, without
// connecting to a database, payment provider or public object storage.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"
	"service/component"
)

func main() {
	input := flag.String("input", "", "报告快照 JSON 路径")
	output := flag.String("output", "", "输出 PDF 路径（必须尚不存在）")
	mode := flag.String("mode", "preview", "preview 或 full")
	python := flag.String("python", "", "Python 可执行文件绝对路径")
	script := flag.String("script", "", "reportpdf/render.py 绝对路径")
	font := flag.String("font", "", "中文 TTF 字体绝对路径")
	assetRoot := flag.String("asset-root", "", "审核图片根目录绝对路径（正式报告必填）")
	flag.Parse()
	if *input == "" || *output == "" {
		log.Fatal("必须指定输入和输出路径")
	}
	renderer, err := component.NewReportPDFRenderer(component.ReportPDFConfig{
		PythonPath: *python, ScriptPath: *script, FontPath: *font, AssetRoot: *assetRoot,
	})
	if err != nil {
		log.Fatalf("渲染配置无效: %v", err)
	}
	snapshot, err := os.ReadFile(*input)
	if err != nil {
		log.Fatal("无法读取报告快照")
	}
	data, err := renderer.Render(context.Background(), snapshot, *mode)
	if err != nil {
		log.Fatalf("报告渲染失败: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(*output), 0o700); err != nil {
		log.Fatal("无法创建报告输出目录")
	}
	f, err := os.OpenFile(*output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		log.Fatal("无法创建报告文件；不覆盖已有文件")
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		if err := os.Remove(*output); err != nil {
			log.Fatal("保存失败，未能清理本次创建的不完整文件")
		}
		log.Fatal("保存报告失败，已清理本次创建的不完整文件")
	}
	log.Printf("报告已生成，大小 %d 字节；未上传或发送", len(data))
}
