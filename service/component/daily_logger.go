package component

import (
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type DailyLogWriter struct {
	mu          sync.Mutex
	baseDir     string
	currentDate string
	file        *os.File
}

func InitDailyAppLogger() (*DailyLogWriter, error) {
	baseDir, err := detectDailyLogBaseDir()
	if err != nil {
		return nil, err
	}
	writer := &DailyLogWriter{
		baseDir: filepath.Join(baseDir, "APPlog"),
	}
	if err := writer.rotateFor(time.Now()); err != nil {
		return nil, err
	}
	log.SetOutput(writer)
	gin.DefaultWriter = writer
	gin.DefaultErrorWriter = writer
	return writer, nil
}

func detectDailyLogBaseDir() (string, error) {
	workingDir, err := os.Getwd()
	if err == nil && workingDir != "" {
		return workingDir, nil
	}
	executablePath, execErr := os.Executable()
	if execErr != nil {
		if err != nil {
			return "", err
		}
		return "", execErr
	}
	return filepath.Dir(executablePath), nil
}

func (w *DailyLogWriter) Directory() string {
	if w == nil {
		return ""
	}
	return w.baseDir
}

func (w *DailyLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.rotateFor(time.Now()); err != nil {
		return 0, err
	}
	return w.file.Write(p)
}

func (w *DailyLogWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *DailyLogWriter) rotateFor(now time.Time) error {
	dateText := now.Format("2006-01-02")
	if w.file != nil && w.currentDate == dateText {
		return nil
	}
	if err := os.MkdirAll(w.baseDir, 0755); err != nil {
		return err
	}
	if w.file != nil {
		if err := w.file.Close(); err != nil {
			return err
		}
		w.file = nil
	}
	logPath := filepath.Join(w.baseDir, dateText+".log")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	w.file = file
	w.currentDate = dateText
	return nil
}
