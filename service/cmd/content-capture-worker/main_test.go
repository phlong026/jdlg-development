package main

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSettingsDefaultsToLoopbackAndKeepsRuntimeDisabledWithoutAllGates(t *testing.T) {
	values := map[string]string{
		"CONTENT_CAPTURE_SECRET":           "12345678901234567890123456789012",
		"CONTENT_CAPTURE_ENABLED":          "true",
		"CONTENT_CAPTURE_NETWORK_ISOLATED": "false",
		"CONTENT_CAPTURE_CDP_URL":          "ws://127.0.0.1:9222/devtools/browser/isolated",
	}
	configuration, err := loadSettings(func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	if configuration.listenAddress != "127.0.0.1:19091" || !configuration.enabled || configuration.networkGate {
		t.Fatalf("settings=%#v", configuration)
	}
}

func TestLoadSettingsRequiresStrongSecretAndLoopback(t *testing.T) {
	base := map[string]string{"CONTENT_CAPTURE_SECRET": "12345678901234567890123456789012"}
	for name, mutate := range map[string]func(map[string]string){
		"secret": func(values map[string]string) { values["CONTENT_CAPTURE_SECRET"] = "short" },
		"public": func(values map[string]string) { values["CONTENT_CAPTURE_LISTEN"] = "0.0.0.0:19091" },
		"host":   func(values map[string]string) { values["CONTENT_CAPTURE_LISTEN"] = "localhost:19091" },
	} {
		t.Run(name, func(t *testing.T) {
			values := map[string]string{}
			for key, value := range base {
				values[key] = value
			}
			mutate(values)
			if _, err := loadSettings(func(key string) string { return values[key] }); err == nil {
				t.Fatal("expected settings error")
			}
		})
	}
}

func TestLoadSettingsAcceptsDedicatedUnixSocketAndDiscovery(t *testing.T) {
	values := map[string]string{
		"CONTENT_CAPTURE_SECRET":            "12345678901234567890123456789012",
		"CONTENT_CAPTURE_LISTEN":            "unix:/run/jdlg-content-capture/capture.sock",
		"CONTENT_CAPTURE_CDP_DISCOVERY_URL": "http://127.0.0.1:9222/json/version",
	}
	configuration, err := loadSettings(func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	if configuration.cdpDiscoveryURL != values["CONTENT_CAPTURE_CDP_DISCOVERY_URL"] {
		t.Fatalf("settings=%#v", configuration)
	}
}

func TestUnixListenerRefusesSymlinksAndExistingSockets(t *testing.T) {
	root := shortTempDir(t)
	realDirectory := filepath.Join(root, "runtime")
	if err := os.Mkdir(realDirectory, 0750); err != nil {
		t.Fatal(err)
	}
	symlinkDirectory := filepath.Join(root, "linked")
	if err := os.Symlink(realDirectory, symlinkDirectory); err != nil {
		t.Fatal(err)
	}
	if _, err := listenUnixCapture(filepath.Join(symlinkDirectory, "capture.sock"), symlinkDirectory); err == nil {
		t.Fatal("symlink directory accepted")
	}

	path := filepath.Join(realDirectory, "capture.sock")
	first, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if _, err := listenUnixCapture(path, realDirectory); err == nil {
		t.Fatal("existing active socket accepted")
	}
}

func TestUnixListenerCreatesSocketWithRestrictedMode(t *testing.T) {
	directory := filepath.Join(shortTempDir(t), "runtime")
	if err := os.Mkdir(directory, 0750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "capture.sock")
	listener, err := listenUnixCapture(path, directory)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0660 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
}

func shortTempDir(t *testing.T) string {
	t.Helper()
	directory, err := os.MkdirTemp("/tmp", "jdlg-capture-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	return directory
}
