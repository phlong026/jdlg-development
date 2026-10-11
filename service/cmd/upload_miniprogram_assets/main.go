package main

import (
    "bufio"
    "context"
    "flag"
    "fmt"
    "io"
    "mime"
    "net/http"
    "os"
    "path/filepath"
    "strings"

    "service/component"
    "service/config"
    "service/function"
)

func main() {
    envPath := flag.String("env", filepath.Join("bin", ".env"), "env file path")
    sourceDir := flag.String("src", filepath.Join("..", "miniprogram", "miniprogram", "assets"), "source assets directory")
    domain := flag.String("domain", "https://static.jiadilingguang.com", "cos custom domain")
    prefix := flag.String("prefix", "", "cos key prefix")
    dryRun := flag.Bool("dry-run", false, "print files only")
    flag.Parse()

    if err := loadEnvFile(*envPath); err != nil {
        fail(err)
    }

    absSourceDir, err := filepath.Abs(*sourceDir)
    if err != nil {
        fail(err)
    }

    if _, err := os.Stat(absSourceDir); err != nil {
        fail(err)
    }

    cfg := config.Init()
    cfg.COS.Domain = strings.TrimSpace(*domain)
    cfg.COS.Prefix = strings.TrimSpace(*prefix)

    if err := component.HealthCheck(cfg); err != nil {
        fail(err)
    }

    client, err := component.InitCOSClient(cfg, nil)
    if err != nil {
        fail(err)
    }

    ctx := context.Background()
    uploadedCount := 0
    uploadedBytes := int64(0)

    err = filepath.Walk(absSourceDir, func(path string, info os.FileInfo, walkErr error) error {
        if walkErr != nil {
            return walkErr
        }
        if info.IsDir() {
            return nil
        }

        rel, err := filepath.Rel(absSourceDir, path)
        if err != nil {
            return err
        }
        key := filepath.ToSlash(filepath.Join("assets", rel))

        if *dryRun {
            fmt.Printf("DRY_RUN %s -> %s\n", path, key)
            return nil
        }

        file, err := os.Open(path)
        if err != nil {
            return err
        }
        defer file.Close()

        contentType, err := detectContentType(file, path)
        if err != nil {
            return err
        }

        if _, err := file.Seek(0, io.SeekStart); err != nil {
            return err
        }

        url, err := function.UploadReader(ctx, client, cfg, key, file, contentType)
        if err != nil {
            return fmt.Errorf("upload failed for %s: %w", key, err)
        }

        uploadedCount++
        uploadedBytes += info.Size()
        fmt.Printf("UPLOADED %s -> %s\n", key, url)
        return nil
    })
    if err != nil {
        fail(err)
    }

    fmt.Printf("DONE files=%d bytes=%d source=%s\n", uploadedCount, uploadedBytes, absSourceDir)
}

func loadEnvFile(path string) error {
    file, err := os.Open(path)
    if err != nil {
        return err
    }
    defer file.Close()

    scanner := bufio.NewScanner(file)
    for scanner.Scan() {
        line := strings.TrimSpace(scanner.Text())
        if line == "" || strings.HasPrefix(line, "#") {
            continue
        }
        if strings.HasPrefix(line, "export ") {
            line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
        }
        parts := strings.SplitN(line, "=", 2)
        if len(parts) != 2 {
            continue
        }
        key := strings.TrimSpace(parts[0])
        value := strings.TrimSpace(parts[1])
        value = strings.Trim(value, "\"'")
        if key == "" {
            continue
        }
        if err := os.Setenv(key, value); err != nil {
            return err
        }
    }
    return scanner.Err()
}

func detectContentType(file *os.File, path string) (string, error) {
    extType := strings.TrimSpace(mime.TypeByExtension(strings.ToLower(filepath.Ext(path))))
    if extType != "" {
        return extType, nil
    }

    buf := make([]byte, 512)
    n, err := file.Read(buf)
    if err != nil && err != io.EOF {
        return "", err
    }
    if _, err := file.Seek(0, io.SeekStart); err != nil {
        return "", err
    }
    return http.DetectContentType(buf[:n]), nil
}

func fail(err error) {
    fmt.Fprintln(os.Stderr, err)
    os.Exit(1)
}
