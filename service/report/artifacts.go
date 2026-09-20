package report

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

const maxCachedPDFBytes = 64 << 20

// CachedRenderer keeps a verified PDF through an upload failure. All instances
// must mount the same private directory if they should reuse each other's PDFs.
type CachedRenderer struct {
	renderer  Renderer
	directory string
	version   string
}

func NewCachedRenderer(renderer Renderer, directory, version string) (*CachedRenderer, error) {
	if renderer == nil || !filepath.IsAbs(directory) || version == "" {
		return nil, ErrInvalidInput
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, artifactFailure("directory")
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return nil, artifactFailure("stat_directory")
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, ErrInvalidInput
	}
	return &CachedRenderer{renderer: renderer, directory: directory, version: version}, nil
}

func (r *CachedRenderer) Render(ctx context.Context, snapshot []byte, mode string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !json.Valid(snapshot) || len(snapshot) > 8<<20 || (mode != ModePreview && mode != ModeFull) {
		return nil, ErrInvalidInput
	}
	// Snapshot already includes report identity and the frozen pack version.
	// Version also fingerprints the installed renderer/font/config in the route.
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s\x00%s\x00", r.version, mode)
	_, _ = h.Write(snapshot)
	key := hex.EncodeToString(h.Sum(nil))
	path := filepath.Join(r.directory, key+".artifact")
	if data, err := readCachedPDF(path); err == nil {
		return data, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	pdf, err := r.renderer.Render(ctx, snapshot, mode)
	if err != nil {
		return nil, err
	}
	if len(pdf) == 0 || len(pdf) > maxCachedPDFBytes || !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		return nil, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(r.directory, ".pending-")
	if err != nil {
		return nil, artifactFailure("create")
	}
	temporary := file.Name()
	defer func() {
		if err := os.Remove(temporary); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("[report] warn artifact_cleanup=failed")
		}
	}()
	digest := sha256.Sum256(pdf)
	data := append([]byte(hex.EncodeToString(digest[:])+"\n"), pdf...)
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return nil, artifactFailure("write")
	}
	if err := os.Rename(temporary, path); err != nil {
		return nil, artifactFailure("publish")
	}
	return pdf, nil
}

func readCachedPDF(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err != nil {
		return nil, artifactFailure("stat")
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > maxCachedPDFBytes+65 {
		return nil, artifactFailure("invalid_file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, artifactFailure("open")
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxCachedPDFBytes+66))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return nil, artifactFailure("read")
	}
	if len(data) < 70 || len(data) > maxCachedPDFBytes+65 || data[64] != '\n' || !bytes.HasPrefix(data[65:], []byte("%PDF-")) {
		return nil, artifactFailure("invalid_file")
	}
	digest := sha256.Sum256(data[65:])
	if string(data[:64]) != hex.EncodeToString(digest[:]) {
		return nil, artifactFailure("checksum")
	}
	return data[65:], nil
}

func artifactFailure(operation string) error {
	log.Printf("[report] error artifact_operation=%s result=failed", operation)
	return ErrNotReady
}
