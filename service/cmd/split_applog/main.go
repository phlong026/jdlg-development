package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	plainDatePattern = regexp.MustCompile(`^(\d{4}/\d{2}/\d{2})\s`)
	ginDatePattern   = regexp.MustCompile(`^\[GIN\]\s+(\d{4}/\d{2}/\d{2})\s+-`)
)

func main() {
	inputPath := flag.String("input", "app.log", "待拆分的原始日志文件路径")
	outputDir := flag.String("output", "APPlog", "拆分后的输出目录")
	flag.Parse()

	if err := splitLogFile(*inputPath, *outputDir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("日志拆分完成，输出目录: %s\n", *outputDir)
}

func splitLogFile(inputPath, outputDir string) error {
	inputFile, err := os.Open(inputPath)
	if err != nil {
		return fmt.Errorf("打开原始日志失败: %w", err)
	}
	defer inputFile.Close()

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("创建输出目录失败: %w", err)
	}

	scanner := bufio.NewScanner(inputFile)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	var currentDate string
	var currentFile *os.File
	defer func() {
		if currentFile != nil {
			currentFile.Close()
		}
	}()

	for scanner.Scan() {
		line := scanner.Text()
		matchedDate := extractDate(line)
		if matchedDate != "" && matchedDate != currentDate {
			if currentFile != nil {
				if err := currentFile.Close(); err != nil {
					currentFile = nil
					currentDate = ""
					continue
				}
			}
			currentDate = matchedDate
			filePath := filepath.Join(outputDir, currentDate+".log")
			currentFile, err = os.OpenFile(filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
			if err != nil {
				return fmt.Errorf("打开日期日志文件失败(%s): %w", filePath, err)
			}
		}

		if currentFile == nil {
			filePath := filepath.Join(outputDir, "unknown.log")
			currentFile, err = os.OpenFile(filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
			if err != nil {
				return fmt.Errorf("打开 unknown 日志文件失败: %w", err)
			}
		}

		if _, err := currentFile.WriteString(line + "\n"); err != nil {
			return fmt.Errorf("写入拆分日志失败: %w", err)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("读取原始日志失败: %w", err)
	}
	return nil
}

func extractDate(line string) string {
	if matches := plainDatePattern.FindStringSubmatch(line); len(matches) == 2 {
		return strings.ReplaceAll(matches[1], "/", "-")
	}
	if matches := ginDatePattern.FindStringSubmatch(line); len(matches) == 2 {
		return strings.ReplaceAll(matches[1], "/", "-")
	}
	return ""
}
