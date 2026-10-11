package model

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

// This driver applies the production UPDATE's SET and compare-and-swap WHERE
// clauses to a fake database row. It has no network or production DB access.
type editDatabase struct {
	row        map[string]driver.Value
	beforeExec func()
	execErr    error
	calls      int
	written    []string
}
type editConnector struct{ db *editDatabase }

func (c editConnector) Connect(context.Context) (driver.Conn, error) {
	return &editConnection{db: c.db}, nil
}
func (c editConnector) Driver() driver.Driver { return editDriver{} }

type editDriver struct{}

func (editDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use connector") }

type editConnection struct{ db *editDatabase }

func (*editConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected Prepare")
}
func (*editConnection) Close() error              { return nil }
func (*editConnection) Begin() (driver.Tx, error) { return nil, errors.New("unexpected transaction") }
func (c *editConnection) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.db.calls++
	if c.db.execErr != nil {
		return nil, c.db.execErr
	}
	if c.db.beforeExec != nil {
		c.db.beforeExec()
	}
	parts := strings.Split(query, " WHERE ")
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "UPDATE templates SET ") {
		return nil, fmt.Errorf("unexpected SQL: %s", query)
	}
	sets := strings.Split(strings.TrimPrefix(parts[0], "UPDATE templates SET "), ", ")
	guards := strings.Split(parts[1], " AND ")
	if len(args) != len(sets)+len(guards) {
		return nil, errors.New("SQL argument count mismatch")
	}
	for index, guard := range guards {
		column := strings.Fields(strings.TrimPrefix(guard, "BINARY "))[0]
		value := args[len(sets)+index].Value
		if boolean, ok := value.(bool); ok {
			value = int64(0)
			if boolean {
				value = int64(1)
			}
		}
		if !reflect.DeepEqual(c.db.row[column], value) {
			return driver.RowsAffected(0), nil
		}
	}
	c.db.written = nil
	for index, assignment := range sets {
		column := strings.Fields(assignment)[0]
		value := args[index].Value
		if boolean, ok := value.(bool); ok {
			value = int64(0)
			if boolean {
				value = int64(1)
			}
		}
		c.db.row[column] = value
		c.db.written = append(c.db.written, column)
	}
	return driver.RowsAffected(1), nil
}
func (c *editConnection) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	parts := strings.Split(query, "FROM templates")
	if len(parts) != 2 {
		return nil, fmt.Errorf("unexpected query %s", query)
	}
	columns := strings.Split(strings.TrimSpace(strings.TrimPrefix(parts[0], "SELECT ")), ",")
	values := make([]driver.Value, len(columns))
	for i := range columns {
		columns[i] = strings.TrimSpace(columns[i])
		values[i] = c.db.row[columns[i]]
	}
	return &editRows{columns: columns, values: values}, nil
}

type editRows struct {
	columns []string
	values  []driver.Value
	read    bool
}

func (r *editRows) Columns() []string { return r.columns }
func (*editRows) Close() error        { return nil }
func (r *editRows) Next(destination []driver.Value) error {
	if r.read {
		return io.EOF
	}
	r.read = true
	copy(destination, r.values)
	return nil
}
func editFixture(t *testing.T) (*TemplateModel, *editDatabase) {
	t.Helper()
	template := Template{ID: 7, Name: "Old name", Category: "villa", MainTab: "main", SubTab: "sub", ThirdTab: "third", Description: "Description", InternalPrompt: "private prompt", Thumbnail: "/thumb.jpg", PreviewURL: "/preview.jpg", Images: `[{"image":"/original.jpg"}]`, ImageWidth: 800, ImageHeight: 1000, Price: 9, IsFree: true, IsFeatured: true, Status: "rejected", PublishScope: "homepage_only", RejectReason: "Needs revision", SourceType: "album_upload", Creator: "Uploader", CreatorUserID: 11, OriginalTaskID: 12, DownloadCount: 5, LikeCount: 6, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	row := map[string]driver.Value{}
	value, typ := reflect.ValueOf(template), reflect.TypeOf(template)
	for i := 0; i < typ.NumField(); i++ {
		if !value.Field(i).CanInterface() {
			continue
		}
		key := typ.Field(i).Tag.Get("json")
		if key == "-" {
			key = "internal_prompt"
		}
		converted, err := driver.DefaultParameterConverter.ConvertValue(value.Field(i).Interface())
		if err != nil {
			t.Fatal(err)
		}
		if boolean, ok := converted.(bool); ok {
			converted = int64(0)
			if boolean {
				converted = int64(1)
			}
		}
		row[key] = converted
	}
	database := &editDatabase{row: row}
	db := sql.OpenDB(editConnector{db: database})
	t.Cleanup(func() { db.Close() })
	return NewTemplateModel(db), database
}
func decodeEdit(t *testing.T, body string) TemplateUpdate {
	t.Helper()
	var update TemplateUpdate
	if err := json.Unmarshal([]byte(body), &update); err != nil {
		t.Fatal(err)
	}
	return update
}
func saveEdit(t *testing.T, m *TemplateModel, body string) error {
	t.Helper()
	before, err := m.GetByID(7)
	if err != nil {
		t.Fatal(err)
	}
	after := *before
	update := decodeEdit(t, body)
	if err := update.Apply(&after); err != nil {
		return err
	}
	return m.UpdatePartial(before, &after, update)
}

func TestTemplateNameOnlyEditPreservesCurrentConcurrentMetadata(t *testing.T) {
	m, db := editFixture(t)
	db.beforeExec = func() {
		db.row["publish_scope"] = "square"
		db.row["reject_reason"] = "A newer review"
		db.row["source_type"] = "ai_generated"
		db.row["status"] = "pending"
		db.row["price"] = int64(99)
		db.row["thumbnail"] = "/new-thumb.jpg"
	}
	if err := saveEdit(t, m, `{"name":"Renamed"}`); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(db.written, []string{"name"}) {
		t.Fatalf("unexpected written columns: %v", db.written)
	}
	current, err := m.GetByID(7)
	if err != nil {
		t.Fatal(err)
	}
	if current.Name != "Renamed" || current.PublishScope != "square" || current.RejectReason != "A newer review" || current.SourceType != "ai_generated" || current.Status != "pending" || current.Price != 99 || current.Thumbnail != "/new-thumb.jpg" {
		t.Fatalf("concurrent metadata lost: %+v", current)
	}
	if current.Creator != "Uploader" || current.OriginalTaskID != 12 || current.InternalPrompt != "private prompt" || current.DownloadCount != 5 || !current.IsFeatured {
		t.Fatalf("uneditable fields changed: %+v", current)
	}
}

func TestTemplateOmittedLegacyMetadataNeverGetsCreationDefaults(t *testing.T) {
	m, db := editFixture(t)
	db.row["publish_scope"] = ""
	db.row["source_type"] = ""
	db.row["reject_reason"] = "  stored reason  "
	if err := saveEdit(t, m, `{"name":"Renamed"}`); err != nil {
		t.Fatal(err)
	}
	if db.row["publish_scope"] != "" || db.row["source_type"] != "" || db.row["reject_reason"] != "  stored reason  " {
		t.Fatal("omitted metadata normalized or defaulted")
	}
}

func TestTemplateExplicitZeroFalseAndEmptyValues(t *testing.T) {
	m, db := editFixture(t)
	if err := saveEdit(t, m, `{"description":"","main_tab":"","sub_tab":"","third_tab":"","price":0,"is_free":false,"reject_reason":""}`); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"description", "main_tab", "sub_tab", "third_tab", "reject_reason"} {
		if db.row[key] != "" {
			t.Fatalf("%s not cleared: %v", key, db.row[key])
		}
	}
	if db.row["price"] != int64(0) || db.row["is_free"] != int64(0) {
		t.Fatalf("zero/false lost: %+v", db.row)
	}
	if db.row["publish_scope"] != "homepage_only" || db.row["source_type"] != "album_upload" {
		t.Fatal("unrelated fields changed")
	}
}

func TestTemplateNullIsRejectedWhileOmissionIsAllowed(t *testing.T) {
	for _, key := range []string{"name", "category", "description", "main_tab", "sub_tab", "third_tab", "thumbnail", "preview_url", "images", "price", "is_free", "status", "publish_scope", "reject_reason", "source_type"} {
		var update TemplateUpdate
		if err := json.Unmarshal([]byte(fmt.Sprintf(`{"%s":null}`, key)), &update); err == nil {
			t.Fatalf("accepted null for %s", key)
		}
	}
	for _, body := range []string{`null`, `[]`, `{"price":"0"}`, `{"is_free":0}`} {
		var update TemplateUpdate
		if err := json.Unmarshal([]byte(body), &update); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
	decodeEdit(t, `{}`)
}

func TestTemplateNoopSkipsSQLRatherThanReportingFalseConflict(t *testing.T) {
	for _, body := range []string{`{}`, `{"name":"Old name","is_free":true,"price":9}`, `{"description":" Description "}`, `{"thumbnail":"/thumb.jpg","preview_url":"/preview.jpg","images":"[{\"image\":\"/original.jpg\"}]"}`} {
		m, db := editFixture(t)
		if err := saveEdit(t, m, body); err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		if db.calls != 0 {
			t.Fatalf("no-op executed SQL for %s", body)
		}
	}
}

func TestTemplateConcurrentEditedValueReturnsConflictWithoutWriting(t *testing.T) {
	m, db := editFixture(t)
	db.beforeExec = func() { db.row["name"] = "Another editor" }
	if err := saveEdit(t, m, `{"name":"My edit"}`); !errors.Is(err, ErrTemplateUpdateConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	if db.row["name"] != "Another editor" || len(db.written) != 0 {
		t.Fatal("concurrent edit was overwritten")
	}
}

func TestTemplateTabValidationGroupHasAtomicConflictProtection(t *testing.T) {
	for _, key := range []string{"main_tab", "sub_tab", "third_tab"} {
		t.Run(key, func(t *testing.T) {
			m, db := editFixture(t)
			db.beforeExec = func() { db.row[key] = "concurrent" }
			if err := saveEdit(t, m, `{"third_tab":"new-third"}`); !errors.Is(err, ErrTemplateUpdateConflict) {
				t.Fatalf("expected %s conflict, got %v", key, err)
			}
			if len(db.written) != 0 {
				t.Fatal("partial tab write")
			}
		})
	}
}

func TestTemplateImageEditsWriteDerivedGroupAndGuardItsInputs(t *testing.T) {
	for _, key := range []string{"", "thumbnail", "preview_url", "images", "image_width", "image_height", "source_type", "creator_user_id"} {
		t.Run("concurrent_"+key, func(t *testing.T) {
			m, db := editFixture(t)
			before, err := m.GetByID(7)
			if err != nil {
				t.Fatal(err)
			}
			after := *before
			update := decodeEdit(t, `{"thumbnail":"/new.jpg","preview_url":"/new.jpg","images":"[\"/new.jpg\"]"}`)
			if err := update.Apply(&after); err != nil {
				t.Fatal(err)
			}
			after.Thumbnail = "/derived-new.jpg"
			after.PreviewURL = "/preview-new.jpg"
			after.ImageWidth = 1200
			after.ImageHeight = 1500
			if key != "" {
				db.beforeExec = func() {
					if _, ok := db.row[key].(int64); ok {
						db.row[key] = int64(222)
					} else {
						db.row[key] = "concurrent"
					}
				}
			}
			err = m.UpdatePartial(before, &after, update)
			if key != "" {
				if !errors.Is(err, ErrTemplateUpdateConflict) {
					t.Fatalf("expected %s conflict, got %v", key, err)
				}
				if len(db.written) != 0 {
					t.Fatal("partial image update")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(db.written, []string{"thumbnail", "preview_url", "images", "image_width", "image_height"}) {
				t.Fatalf("unexpected write group: %v", db.written)
			}
			if db.row["publish_scope"] != "homepage_only" || db.row["reject_reason"] != "Needs revision" {
				t.Fatal("image update changed moderation metadata")
			}
		})
	}
}

func TestTemplatePublicationChangeGuardsValidatedImageAndTabSnapshot(t *testing.T) {
	for _, key := range []string{"main_tab", "third_tab", "thumbnail", "images"} {
		t.Run(key, func(t *testing.T) {
			m, db := editFixture(t)
			db.beforeExec = func() { db.row[key] = "concurrent" }
			if err := saveEdit(t, m, `{"status":"published"}`); !errors.Is(err, ErrTemplateUpdateConflict) {
				t.Fatalf("unsafe publication: %v", err)
			}
		})
	}
}

func TestTemplateDatabaseErrorPropagatesWithoutMutation(t *testing.T) {
	m, db := editFixture(t)
	db.execErr = errors.New("fake DB unavailable")
	if err := saveEdit(t, m, `{"name":"New"}`); !errors.Is(err, db.execErr) {
		t.Fatalf("expected DB error, got %v", err)
	}
	if db.row["name"] != "Old name" {
		t.Fatal("failed write changed row")
	}
}

func TestTemplateLegacyDescriptionEditPreservesHiddenPromptAndUsesRawCAS(t *testing.T) {
	for _, stored := range []string{"  Description  ", "  Description\n\n提示词: secret prompt  "} {
		t.Run(stored, func(t *testing.T) {
			m, db := editFixture(t)
			db.row["description"] = stored
			db.row["internal_prompt"] = nil
			if err := saveEdit(t, m, `{"description":"Updated description"}`); err != nil {
				t.Fatalf("valid legacy edit failed: %v", err)
			}
			current, err := m.GetByID(7)
			if err != nil {
				t.Fatal(err)
			}
			if current.Description != "Updated description" {
				t.Fatalf("wrong public description: %s", current.Description)
			}
			if strings.Contains(stored, LegacyTemplatePromptPrefix) && (current.InternalPrompt != "secret prompt" || db.row["description"] != "Updated description\n\n提示词: secret prompt  ") {
				t.Fatalf("hidden legacy prompt lost: %+v, raw %v", current, db.row["description"])
			}
			if db.row["internal_prompt"] != nil || !reflect.DeepEqual(db.written, []string{"description"}) {
				t.Fatalf("unrequested internal metadata changed: %v", db.written)
			}
		})
	}
}

func TestTemplateLegacyNoopPreservesRawDescriptionAndPrompt(t *testing.T) {
	m, db := editFixture(t)
	raw := "  Description\n\n提示词: secret prompt  "
	db.row["description"] = raw
	db.row["internal_prompt"] = ""
	if err := saveEdit(t, m, `{"description":"Description"}`); err != nil {
		t.Fatal(err)
	}
	if db.calls != 0 || db.row["description"] != raw || db.row["internal_prompt"] != "" {
		t.Fatal("legacy no-op changed storage")
	}
}

func TestTemplateConcurrentLegacyPromptEditReturnsConflict(t *testing.T) {
	m, db := editFixture(t)
	db.row["description"] = "Description\n\n提示词: secret prompt"
	db.row["internal_prompt"] = ""
	db.beforeExec = func() { db.row["description"] = "Description\n\n提示词: newer private prompt" }
	if err := saveEdit(t, m, `{"description":"Updated"}`); !errors.Is(err, ErrTemplateUpdateConflict) {
		t.Fatalf("expected legacy prompt conflict: %v", err)
	}
	if db.row["description"] != "Description\n\n提示词: newer private prompt" {
		t.Fatal("newer hidden prompt was overwritten")
	}
}

func TestTemplateClearingLegacyPublicDescriptionNeverExposesHiddenPrompt(t *testing.T) {
	for _, body := range []string{`{"description":""}`, `{"description":"  "}`} {
		t.Run(body, func(t *testing.T) {
			m, db := editFixture(t)
			db.row["description"] = "Description\n\n提示词: secret prompt  "
			db.row["internal_prompt"] = ""
			if err := saveEdit(t, m, body); err != nil {
				t.Fatal(err)
			}
			current, err := m.GetByID(7)
			if err != nil {
				t.Fatal(err)
			}
			if current.Description != "" || current.InternalPrompt != "secret prompt" {
				t.Fatalf("empty description broke private/public split: %+v", current)
			}
			encoded, err := json.Marshal(current)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "secret prompt") || strings.Contains(string(encoded), "提示词") {
				t.Fatalf("private prompt leaked in response: %s", encoded)
			}
			if db.row["description"] != "\n\n提示词: secret prompt  " || db.row["internal_prompt"] != "" {
				t.Fatal("private storage was not preserved")
			}
		})
	}
}

func TestTemplateLegacySuffixStaysPrivateWithSeparateInternalPrompt(t *testing.T) {
	m, db := editFixture(t)
	db.row["description"] = "\n\n提示词: old secret"
	db.row["internal_prompt"] = "new secret"
	current, err := m.GetByID(7)
	if err != nil {
		t.Fatal(err)
	}
	if current.Description != "" || current.InternalPrompt != "new secret" {
		t.Fatalf("wrong prompt precedence or public text: %+v", current)
	}
	encoded, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secret") {
		t.Fatalf("private prompt leaked: %s", encoded)
	}
}
