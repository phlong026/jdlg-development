package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWechatSecretHasNoSourceFallback(t *testing.T) {
	t.Setenv("WECHAT_APPSECRET", "")
	if Init().Wechat.AppSecret != "" {
		t.Fatal("missing credentials must not silently use a source-code secret")
	}
	t.Setenv("WECHAT_APPSECRET", "unit-test-secret")
	if Init().Wechat.AppSecret != "unit-test-secret" {
		t.Fatal("explicit deployment credentials must still work")
	}
}

func TestLocalEnvironmentPreservesDeploymentValues(t *testing.T) {
	const newKey = "JDLG_TEST_FILE_ONLY"
	os.Unsetenv(newKey)
	t.Cleanup(func() { os.Unsetenv(newKey) })
	t.Setenv("JDLG_TEST_ALREADY_SET", "deployment-value")
	t.Setenv("JDLG_TEST_EMPTY", "")
	p := filepath.Join(t.TempDir(), ".env.local")
	content := "# local settings\n" + newKey + "='local-value'\nJDLG_TEST_ALREADY_SET=file-value\nJDLG_TEST_EMPTY=must-not-override\n"
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if err := LoadLocalEnvFile(p); err != nil {
		t.Fatal(err)
	}
	if os.Getenv(newKey) != "local-value" || os.Getenv("JDLG_TEST_ALREADY_SET") != "deployment-value" || os.Getenv("JDLG_TEST_EMPTY") != "" {
		t.Fatal("local fallback must not overwrite even explicitly empty deployment values")
	}
}

func TestInvalidLocalEnvironmentDoesNotLeakValuesOrPartiallyApply(t *testing.T) {
	const key = "JDLG_TEST_ATOMIC"
	os.Unsetenv(key)
	t.Cleanup(func() { os.Unsetenv(key) })
	p := filepath.Join(t.TempDir(), ".env.local")
	if err := os.WriteFile(p, []byte(key+"=unit-test-secret\ninvalid line containing unit-test-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	err := LoadLocalEnvFile(p)
	if err == nil || strings.Contains(err.Error(), "unit-test-secret") || os.Getenv(key) != "" {
		t.Fatal("invalid input must fail without exposing secrets or applying a partial file")
	}
}
