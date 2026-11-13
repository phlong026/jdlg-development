package route

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	"service/model"
)

func TestStudioMediaProbesActualImageAndRejectsFalseType(t *testing.T) {
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 12, 8))); err != nil {
		t.Fatal(err)
	}
	info, err := inspectStudioMedia(context.Background(), data.Bytes(), "poster.png")
	if err != nil || info.Kind != "image" || info.Width != 12 || info.Height != 8 {
		t.Fatalf("info=%+v err=%v", info, err)
	}
	if _, err := inspectStudioMedia(context.Background(), []byte("a file claiming to be mp4"), "forged.mp4"); err == nil {
		t.Fatal("forged media accepted")
	}
	if _, err := inspectStudioMedia(context.Background(), data.Bytes()[:20], "truncated.png"); err == nil {
		t.Fatal("truncated media accepted")
	}
}

type studioRetentionDriver struct {
	affected       int64
	status, stable string
}
type studioRetentionConn struct{ fixture studioRetentionDriver }
type studioRetentionRows struct {
	status, stable string
	done           bool
}

func (d studioRetentionDriver) Open(string) (driver.Conn, error) { return studioRetentionConn{d}, nil }
func (studioRetentionConn) Prepare(string) (driver.Stmt, error)  { return nil, driver.ErrSkip }
func (studioRetentionConn) Close() error                         { return nil }
func (studioRetentionConn) Begin() (driver.Tx, error)            { return nil, driver.ErrSkip }
func (c studioRetentionConn) ExecContext(_ context.Context, _ string, args []driver.NamedValue) (driver.Result, error) {
	if len(args) != 4 || args[2].Value != int64(1001) {
		return nil, errors.New("owner predicate missing")
	}
	return driver.RowsAffected(c.fixture.affected), nil
}
func (c studioRetentionConn) QueryContext(_ context.Context, _ string, args []driver.NamedValue) (driver.Rows, error) {
	if len(args) != 2 || args[1].Value != int64(1001) {
		return nil, errors.New("owner predicate missing")
	}
	return &studioRetentionRows{status: c.fixture.status, stable: c.fixture.stable}, nil
}
func (*studioRetentionRows) Columns() []string {
	return []string{"status", "result_url", "stable_media_id"}
}
func (*studioRetentionRows) Close() error { return nil }
func (r *studioRetentionRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0] = r.status
	dest[1] = ""
	dest[2] = r.stable
	return nil
}

func TestStudioRetentionDoesNotReportConflictingAttachmentAsSuccess(t *testing.T) {
	for _, tc := range []struct {
		fixture  studioRetentionDriver
		conflict bool
	}{
		{studioRetentionDriver{affected: 1}, false},
		{studioRetentionDriver{status: "completed", stable: "same-media"}, false},
		{studioRetentionDriver{status: "completed", stable: "other-media"}, true},
		{studioRetentionDriver{status: "failed", stable: "same-media"}, true},
	} {
		name := fmt.Sprintf("studio-retention-%d", time.Now().UnixNano())
		sql.Register(name, tc.fixture)
		db, err := sql.Open(name, "")
		if err != nil {
			t.Fatal(err)
		}
		err = attachStudioRetainedMedia(context.Background(), model.NewAIVideoStudioStore(db), 1001, "task-1", "same-media")
		db.Close()
		if errors.Is(err, model.ErrAIVideoStudioConflict) != tc.conflict || (!tc.conflict && err != nil) {
			t.Fatalf("fixture=%+v error=%v", tc.fixture, err)
		}
	}
}

func TestStudioRetainedResponseExplicitlyMarksCompletion(t *testing.T) {
	body, err := json.Marshal(studioRetainedResponse(&model.AIVideoStudioMedia{ID: "media-1", Status: "ready"}))
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err = json.Unmarshal(body, &response); err != nil {
		t.Fatal(err)
	}
	if response["retained"] != true || response["retention_status"] != "retained" || response["delivery_type"] != "retained" || response["media_id"] != "media-1" {
		t.Fatalf("ambiguous response: %s", body)
	}
}

func TestStudioMediaProbesAudioDurationWithoutClientMetadata(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}
	// A real PCM WAV fixture: one channel, 8kHz, 16bit, exactly 2 seconds.
	samples := make([]byte, 32000)
	var wav bytes.Buffer
	wav.WriteString("RIFF")
	binary.Write(&wav, binary.LittleEndian, uint32(36+len(samples)))
	wav.WriteString("WAVEfmt ")
	for _, value := range []any{uint32(16), uint16(1), uint16(1), uint32(8000), uint32(16000), uint16(2), uint16(16)} {
		if err := binary.Write(&wav, binary.LittleEndian, value); err != nil {
			t.Fatal(err)
		}
	}
	wav.WriteString("data")
	binary.Write(&wav, binary.LittleEndian, uint32(len(samples)))
	wav.Write(samples)
	info, err := inspectStudioMedia(context.Background(), wav.Bytes(), "reference.wav")
	if err != nil || info.Kind != "audio" || info.DurationSeconds != 2 {
		t.Fatalf("info=%+v err=%v", info, err)
	}
}

func TestStudioMediaSignedURLRequiresSecretAndDomainSeparation(t *testing.T) {
	id := strings.Repeat("a", 32)
	t.Setenv("TRACKING_MEDIA_SIGN_KEY", "")
	if _, err := studioMediaSignedURL(id, time.Now().Add(time.Minute)); err == nil {
		t.Fatal("predictable fallback allowed")
	}
	t.Setenv("TRACKING_MEDIA_SIGN_KEY", strings.Repeat("test-only-", 4))
	expires := time.Now().Add(time.Minute)
	link, err := studioMediaSignedURL(id, expires)
	if err != nil || !strings.HasPrefix(link, "https://api.jiadilingguang.com/api/v1/ai-video-studio-media/") {
		t.Fatalf("link=%s err=%v", link, err)
	}
	if trackingMediaSignature(id, expires.Unix()) == trackingMediaSignature("studio:"+id, expires.Unix()) {
		t.Fatal("signature domain reused")
	}
	if _, err := studioMediaSignedURL("../secret", expires); err == nil {
		t.Fatal("invalid storage id accepted")
	}
}

func TestStudioRetentionRefusesUnsafeURLsBeforeDialing(t *testing.T) {
	for _, raw := range []string{"http://cdn.example.com/video.mp4", "https://127.0.0.1/private", "https://169.254.169.254/meta", "https://user:password@cdn.example.com/video.mp4", "https://cdn.example.com:8443/video.mp4"} {
		if _, err := downloadStudioResult(context.Background(), raw); err == nil {
			t.Fatalf("unsafe source accepted: %s", raw)
		}
	}
}
