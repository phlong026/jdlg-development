package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadLocalEnv keeps local credentials outside source control; deployment
// variables remain authoritative, including values explicitly set to empty.
func LoadLocalEnv() error {
	if p := os.Getenv("JDLG_LOCAL_ENV_FILE"); p != "" {
		return LoadLocalEnvFile(p)
	}
	paths := []string{".env.local"}
	if executable, err := os.Executable(); err == nil {
		paths = append(paths, filepath.Join(filepath.Dir(executable), ".env.local"))
	}
	for _, p := range paths {
		err := LoadLocalEnvFile(p)
		if os.IsNotExist(err) {
			continue
		}
		return err
	}
	return nil
}

// LoadLocalEnvFile accepts literal KEY=value lines, not executable shell code.
// Validate the entire file first so a typo cannot apply half a configuration.
func LoadLocalEnvFile(p string) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	values := map[string]string{}
	scanner := bufio.NewScanner(f)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !found || !validEnvKey(key) {
			return fmt.Errorf("invalid local environment assignment at line %d", lineNumber)
		}
		if _, duplicate := values[key]; duplicate {
			return fmt.Errorf("duplicate local environment assignment at line %d", lineNumber)
		}
		if len(value) > 0 && (value[0] == '\'' || value[0] == '"') {
			if len(value) < 2 || value[len(value)-1] != value[0] {
				return fmt.Errorf("unclosed local environment quote at line %d", lineNumber)
			}
			value = value[1 : len(value)-1]
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read local environment: %w", err)
	}
	for key, value := range values {
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, value); err != nil {
				return fmt.Errorf("could not apply local environment variable %s", key)
			}
		}
	}
	return nil
}

func validEnvKey(key string) bool {
	if key == "" {
		return false
	}
	for i, c := range key {
		if c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || i > 0 && c >= '0' && c <= '9' {
			continue
		}
		return false
	}
	return true
}
