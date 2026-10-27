package main

import "testing"

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
