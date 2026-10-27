package contentcapture

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"os"
	"os/user"
	"reflect"
	"runtime"
	"service/contentimport"
	"strconv"
	"strings"
	"testing"
	"time"
)

const liveBrowserProfile = "jdlg-content-capture-browser"

const (
	liveMainServiceEnvironment = "/opt/jdlg-content-capture/main-service.env"
	liveCaptureEndpoint        = "https://api.jiadilingguang.com/_internal/content-capture/v1/capture"
	liveHealthEndpoint         = "https://api.jiadilingguang.com/_internal/content-capture/ready"
	liveCaptureDialAddress     = "127.0.0.1:443"
	maxLiveEnvironmentBytes    = 16 << 10
)

type procSecurityState struct {
	uid          int
	seccomp      int
	noNewPrivs   int
	capEffIsZero bool
}

func TestParseProcSecurityState(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want procSecurityState
		ok   bool
	}{
		{
			name: "hardened renderer",
			raw:  "Name:\trenderer\nUid:\t1001\t1001\t1001\t1001\nCapEff:\t0000000000000000\nNoNewPrivs:\t1\nSeccomp:\t2\n",
			want: procSecurityState{uid: 1001, seccomp: 2, noNewPrivs: 1, capEffIsZero: true},
			ok:   true,
		},
		{
			name: "effective capability present",
			raw:  "Uid: 2002 2002 2002 2002\nCapEff: 0000000000000400\nNoNewPrivs: 1\nSeccomp: 2\n",
			want: procSecurityState{uid: 2002, seccomp: 2, noNewPrivs: 1, capEffIsZero: false},
			ok:   true,
		},
		{
			name: "missing field",
			raw:  "Uid: 1001 1001 1001 1001\nCapEff: 0\nSeccomp: 2\n",
			ok:   false,
		},
		{
			name: "invalid number",
			raw:  "Uid: browser browser browser browser\nCapEff: 0\nNoNewPrivs: 1\nSeccomp: 2\n",
			ok:   false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := parseProcSecurityState([]byte(test.raw))
			if ok != test.ok || got != test.want {
				t.Fatalf("state=%#v ok=%t", got, ok)
			}
		})
	}
}

func TestSummarizeRendererSecurity(t *testing.T) {
	secure := rendererSecurityObservation{
		procReadable:    true,
		security:        procSecurityState{uid: 1001, seccomp: 2, noNewPrivs: 1, capEffIsZero: true},
		profileReadable: true,
		profile:         liveBrowserProfile + "//renderer",
	}
	tests := []struct {
		name        string
		commandLine string
		renderers   []rendererSecurityObservation
		valid       bool
	}{
		{name: "hardened renderer", commandLine: "/usr/bin/chromium --headless", renderers: []rendererSecurityObservation{secure}, valid: true},
		{name: "no renderer", commandLine: "/usr/bin/chromium --headless", valid: false},
		{name: "sandbox disabled", commandLine: "/usr/bin/chromium --headless --no-sandbox", renderers: []rendererSecurityObservation{secure}, valid: false},
		{name: "empty browser command line", renderers: []rendererSecurityObservation{secure}, valid: false},
		{name: "unreadable proc state", commandLine: "/usr/bin/chromium --headless", renderers: []rendererSecurityObservation{{}}, valid: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			summary := summarizeRendererSecurity(test.commandLine, 1001, test.renderers)
			if summary.valid() != test.valid {
				t.Fatalf("valid=%t", summary.valid())
			}
		})
	}
}

func TestLiveIsolatedBrowserDiagnostics(t *testing.T) {
	if os.Getenv("JDLG_CAPTURE_BROWSER_LIVE") != "true" {
		t.Skip("live=false")
	}
	if runtime.GOOS != "linux" {
		t.Skip("linux=false")
	}
	currentUser, err := user.Current()
	requireLiveProbe(t, "root", err == nil && currentUser.Uid == "0")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	endpoint, err := discoverCDPEndpoint(ctx, newDiscoveryClient(), "http://127.0.0.1:9222/json/version")
	requireLiveProbe(t, "discover", err == nil)
	connection, err := (WebSocketDialer{}).Dial(ctx, endpoint)
	requireLiveProbe(t, "dial", err == nil)
	defer connection.Close()

	var version struct {
		Product string `json:"product"`
	}
	err = connection.Call(ctx, "", "Browser.getVersion", map[string]any{}, &version)
	requireLiveProbe(t, "version", err == nil && version.Product != "")

	var browserContext struct {
		ID string `json:"browserContextId"`
	}
	err = connection.Call(ctx, "", "Target.createBrowserContext", map[string]any{"disposeOnDetach": true}, &browserContext)
	requireLiveProbe(t, "context", err == nil && browserContext.ID != "")
	defer cleanupCall(connection, "Target.disposeBrowserContext", map[string]any{"browserContextId": browserContext.ID})

	html := `<html><body data-jdlg-live="ready">capture diagnostic</body></html>`
	pageURL := "data:text/html;base64," + base64.StdEncoding.EncodeToString([]byte(html))
	var target struct {
		ID string `json:"targetId"`
	}
	err = connection.Call(ctx, "", "Target.createTarget", map[string]any{"url": pageURL, "browserContextId": browserContext.ID}, &target)
	requireLiveProbe(t, "target", err == nil && target.ID != "")
	defer cleanupCall(connection, "Target.closeTarget", map[string]any{"targetId": target.ID})

	var attached struct {
		SessionID string `json:"sessionId"`
	}
	err = connection.Call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": target.ID, "flatten": true}, &attached)
	requireLiveProbe(t, "attach", err == nil && attached.SessionID != "")
	defer cleanupCall(connection, "Target.detachFromTarget", map[string]any{"sessionId": attached.SessionID})

	rendered := waitForDiagnosticPage(ctx, connection, attached.SessionID)
	requireLiveProbe(t, "rendered", rendered)

	state := inspectRendererSecurity(ctx, t, connection)
	t.Logf("renderers=%d proc_readable=%t uid_match=%t seccomp=%t no_new_privs=%t caps_dropped=%t sandbox_flag_absent=%t apparmor=%t", state.count, state.procReadable, state.uidMatch, state.seccomp, state.noNewPrivs, state.capabilitiesDropped, state.sandboxFlagAbsent, state.apparmor)
	if !state.valid() {
		t.Fail()
	}
}

func TestLiveCaptureHTTPSHealth(t *testing.T) {
	if os.Getenv("JDLG_CAPTURE_HTTPS_LIVE") != "true" {
		t.Skip("health_ready=false")
	}
	if runtime.GOOS != "linux" {
		t.Skip("health_ready=false")
	}

	currentUser, userErr := user.Current()
	configuration, valid := readLiveCaptureWorkerConfig()
	valid = valid && userErr == nil && currentUser.Uid == "0"
	ready := false
	if valid {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		worker := contentimport.NewCaptureWorker(configuration)
		ready = worker.Ready() && worker.Probe(ctx) == nil && worker.Available(ctx)
	}
	t.Logf("health_ready=%t", ready)
	if !ready {
		t.Fail()
	}
}

func readLiveCaptureWorkerConfig() (contentimport.CaptureWorkerConfig, bool) {
	pathInfo, err := os.Lstat(liveMainServiceEnvironment)
	if err != nil || !secureRootEnvironmentFile(pathInfo) {
		return contentimport.CaptureWorkerConfig{}, false
	}
	file, err := os.Open(liveMainServiceEnvironment)
	if err != nil {
		return contentimport.CaptureWorkerConfig{}, false
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil || !os.SameFile(pathInfo, openedInfo) || !secureRootEnvironmentFile(openedInfo) {
		return contentimport.CaptureWorkerConfig{}, false
	}
	body, err := io.ReadAll(io.LimitReader(file, maxLiveEnvironmentBytes+1))
	if err != nil || len(body) > maxLiveEnvironmentBytes || bytes.IndexByte(body, 0) >= 0 {
		return contentimport.CaptureWorkerConfig{}, false
	}
	values, ok := parseLiveCaptureEnvironment(string(body))
	if !ok || values["CONTENT_IMPORT_CAPTURE_URL"] != liveCaptureEndpoint || values["CONTENT_IMPORT_CAPTURE_HEALTH_URL"] != liveHealthEndpoint || values["CONTENT_IMPORT_CAPTURE_DIAL_ADDRESS"] != liveCaptureDialAddress || len(values["CONTENT_IMPORT_CAPTURE_SECRET"]) < 32 {
		return contentimport.CaptureWorkerConfig{}, false
	}
	return contentimport.CaptureWorkerConfig{
		Endpoint:       values["CONTENT_IMPORT_CAPTURE_URL"],
		HealthEndpoint: values["CONTENT_IMPORT_CAPTURE_HEALTH_URL"],
		DialAddress:    values["CONTENT_IMPORT_CAPTURE_DIAL_ADDRESS"],
		Secret:         values["CONTENT_IMPORT_CAPTURE_SECRET"],
		Timeout:        20 * time.Second,
	}, true
}

func secureRootEnvironmentFile(info os.FileInfo) bool {
	if info == nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return false
	}
	metadata := reflect.ValueOf(info.Sys())
	if !metadata.IsValid() {
		return false
	}
	if metadata.Kind() == reflect.Pointer {
		if metadata.IsNil() {
			return false
		}
		metadata = metadata.Elem()
	}
	if metadata.Kind() != reflect.Struct {
		return false
	}
	uid := metadata.FieldByName("Uid")
	return uid.IsValid() && uid.CanUint() && uid.Uint() == 0
}

func parseLiveCaptureEnvironment(raw string) (map[string]string, bool) {
	allowed := map[string]bool{
		"CONTENT_IMPORT_CAPTURE_URL":          true,
		"CONTENT_IMPORT_CAPTURE_HEALTH_URL":   true,
		"CONTENT_IMPORT_CAPTURE_DIAL_ADDRESS": true,
		"CONTENT_IMPORT_CAPTURE_SECRET":       true,
	}
	values := make(map[string]string, len(allowed))
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if !found || !allowed[key] || values[key] != "" || value == "" || strings.ContainsAny(value, "\r\n") {
			return nil, false
		}
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		if value == "" {
			return nil, false
		}
		values[key] = value
	}
	return values, len(values) == len(allowed)
}

func waitForDiagnosticPage(ctx context.Context, connection CDPConnection, sessionID string) bool {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		var evaluated struct {
			Result struct {
				Value bool `json:"value"`
			} `json:"result"`
		}
		err := connection.Call(ctx, sessionID, "Runtime.evaluate", map[string]any{
			"expression":    `document.body?.dataset.jdlgLive === "ready"`,
			"returnByValue": true,
		}, &evaluated)
		if err == nil && evaluated.Result.Value {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

type rendererSecuritySummary struct {
	count               int
	procReadable        bool
	uidMatch            bool
	seccomp             bool
	noNewPrivs          bool
	capabilitiesDropped bool
	sandboxFlagAbsent   bool
	apparmor            bool
}

func (summary rendererSecuritySummary) valid() bool {
	return summary.count > 0 && summary.procReadable && summary.uidMatch && summary.seccomp && summary.noNewPrivs && summary.capabilitiesDropped && summary.sandboxFlagAbsent && summary.apparmor
}

type rendererSecurityObservation struct {
	procReadable    bool
	security        procSecurityState
	profileReadable bool
	profile         string
}

func inspectRendererSecurity(ctx context.Context, t *testing.T, connection CDPConnection) rendererSecuritySummary {
	t.Helper()
	account, err := user.Lookup("jdlgcap-browser")
	requireLiveProbe(t, "browser_user", err == nil)
	uid, err := strconv.Atoi(account.Uid)
	requireLiveProbe(t, "browser_uid", err == nil)

	var browserInfo struct {
		CommandLine string `json:"commandLine"`
	}
	err = connection.Call(ctx, "", "SystemInfo.getInfo", map[string]any{}, &browserInfo)
	requireLiveProbe(t, "browser_info", err == nil)
	var processInfo struct {
		Processes []struct {
			Type string `json:"type"`
			ID   int    `json:"id"`
		} `json:"processInfo"`
	}
	err = connection.Call(ctx, "", "SystemInfo.getProcessInfo", map[string]any{}, &processInfo)
	requireLiveProbe(t, "process_info", err == nil)

	renderers := make([]rendererSecurityObservation, 0)
	for _, process := range processInfo.Processes {
		if process.Type != "renderer" || process.ID <= 0 {
			continue
		}
		pid := strconv.Itoa(process.ID)
		observation := rendererSecurityObservation{}
		status, err := os.ReadFile("/proc/" + pid + "/status")
		if err != nil {
			renderers = append(renderers, observation)
			continue
		}
		security, ok := parseProcSecurityState(status)
		if !ok {
			renderers = append(renderers, observation)
			continue
		}
		observation.procReadable = true
		observation.security = security
		profile, err := os.ReadFile("/proc/" + pid + "/attr/current")
		observation.profileReadable = err == nil
		if err == nil {
			observation.profile = string(profile)
		}
		renderers = append(renderers, observation)
	}
	return summarizeRendererSecurity(browserInfo.CommandLine, uid, renderers)
}

func summarizeRendererSecurity(commandLine string, expectedUID int, renderers []rendererSecurityObservation) rendererSecuritySummary {
	summary := rendererSecuritySummary{
		count:               len(renderers),
		procReadable:        true,
		uidMatch:            true,
		seccomp:             true,
		noNewPrivs:          true,
		capabilitiesDropped: true,
		sandboxFlagAbsent:   browserCommandLineSafe(commandLine),
		apparmor:            true,
	}
	for _, renderer := range renderers {
		summary.procReadable = summary.procReadable && renderer.procReadable && renderer.profileReadable
		summary.uidMatch = summary.uidMatch && renderer.procReadable && renderer.security.uid == expectedUID
		summary.seccomp = summary.seccomp && renderer.procReadable && renderer.security.seccomp == 2
		summary.noNewPrivs = summary.noNewPrivs && renderer.procReadable && renderer.security.noNewPrivs == 1
		summary.capabilitiesDropped = summary.capabilitiesDropped && renderer.procReadable && renderer.security.capEffIsZero
		summary.apparmor = summary.apparmor && renderer.profileReadable && strings.Contains(renderer.profile, liveBrowserProfile)
	}
	return summary
}

func browserCommandLineSafe(commandLine string) bool {
	fields := strings.Fields(commandLine)
	if len(fields) == 0 {
		return false
	}
	for _, field := range fields {
		if field == "--no-sandbox" {
			return false
		}
	}
	return true
}

func parseProcSecurityState(raw []byte) (procSecurityState, bool) {
	var state procSecurityState
	foundUID, foundSeccomp, foundNoNewPrivs, foundCapEff := false, false, false, false
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch strings.TrimSuffix(fields[0], ":") {
		case "Uid":
			uid, err := strconv.Atoi(fields[1])
			if err != nil {
				return procSecurityState{}, false
			}
			state.uid, foundUID = uid, true
		case "Seccomp":
			value, err := strconv.Atoi(fields[1])
			if err != nil {
				return procSecurityState{}, false
			}
			state.seccomp, foundSeccomp = value, true
		case "NoNewPrivs":
			value, err := strconv.Atoi(fields[1])
			if err != nil {
				return procSecurityState{}, false
			}
			state.noNewPrivs, foundNoNewPrivs = value, true
		case "CapEff":
			if !validHex(fields[1]) {
				return procSecurityState{}, false
			}
			state.capEffIsZero = strings.Trim(fields[1], "0") == ""
			foundCapEff = true
		}
	}
	if !foundUID || !foundSeccomp || !foundNoNewPrivs || !foundCapEff {
		return procSecurityState{}, false
	}
	return state, true
}

func validHex(raw string) bool {
	if raw == "" {
		return false
	}
	for _, character := range raw {
		if !strings.ContainsRune("0123456789abcdefABCDEF", character) {
			return false
		}
	}
	return true
}

func requireLiveProbe(t *testing.T, name string, ok bool) {
	t.Helper()
	if !ok {
		t.Fatalf("%s=false", name)
	}
}
