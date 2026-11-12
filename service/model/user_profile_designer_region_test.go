package model

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestNormalizeDesignerRegion(t *testing.T) {
	for raw, want := range map[string]string{"": "", " 福建省\t泉州市  丰泽区 ": "福建省 泉州市 丰泽区", strings.Repeat("中", 128): strings.Repeat("中", 128)} {
		got, err := NormalizeDesignerRegion(raw)
		if err != nil || got != want {
			t.Fatalf("region=%q err=%v want=%q", got, err, want)
		}
	}
	for _, raw := range []string{strings.Repeat("中", 129), string([]byte{0xff})} {
		if _, err := NormalizeDesignerRegion(raw); err == nil {
			t.Fatalf("accepted invalid region %q", raw)
		}
	}
}

type designerRegionProfileConnector struct {
	region string
	writes int
}

func (s *designerRegionProfileConnector) Driver() driver.Driver                        { return nil }
func (s *designerRegionProfileConnector) Connect(context.Context) (driver.Conn, error) { return s, nil }
func (*designerRegionProfileConnector) Prepare(string) (driver.Stmt, error) {
	return nil, driver.ErrSkip
}
func (*designerRegionProfileConnector) Close() error              { return nil }
func (*designerRegionProfileConnector) Begin() (driver.Tx, error) { return nil, driver.ErrSkip }
func (s *designerRegionProfileConnector) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if !strings.Contains(query, "designer_region = COALESCE(?, designer_region)") {
		return nil, errors.New("missing preservation of omitted region")
	}
	s.writes++
	if value := args[len(args)-2].Value; value != nil {
		s.region = value.(string)
	}
	return driver.RowsAffected(1), nil
}

type designerRegionReadRows struct {
	region string
	done   bool
}

func (*designerRegionReadRows) Columns() []string { return make([]string, 24) }
func (*designerRegionReadRows) Close() error      { return nil }
func (r *designerRegionReadRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	now := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	copy(dest, []driver.Value{int64(1), int64(7), "昵称", "", "", "庭院设计", r.region, int64(3), "设计师", int64(0), "", int64(1), int64(1), int64(0), nil, "", "", nil, nil, int64(0), "", "设计师", now, now})
	return nil
}
func (s *designerRegionProfileConnector) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if !strings.Contains(query, "COALESCE(designer_region, '') AS designer_region") || args[0].Value != int64(7) {
		return nil, errors.New("region missing from profile projection")
	}
	return &designerRegionReadRows{region: s.region}, nil
}

func TestDesignerRegionProfileProjectionReadsWithoutShiftingOtherFields(t *testing.T) {
	db := sql.OpenDB(&designerRegionProfileConnector{region: "福建省 泉州市 丰泽区"})
	t.Cleanup(func() { db.Close() })
	p, err := NewUserProfileModel(db).GetByUserID(7)
	if err != nil || p.DesignerRegion != "福建省 泉州市 丰泽区" || p.DesignerExperienceYears != 3 || p.ServiceTitle != "设计师" || !p.ServiceEnabled || p.IdentityType != "设计师" {
		t.Fatalf("profile=%+v err=%v", p, err)
	}
}

func TestDesignerProfileUpdatesPreserveOmittedRegionAndAllowExplicitClear(t *testing.T) {
	state := &designerRegionProfileConnector{region: "福建省 泉州市 丰泽区"}
	db := sql.OpenDB(state)
	t.Cleanup(func() { db.Close() })
	m := NewUserProfileModel(db)
	if err := m.UpdateDesignerProfile(7, "简介", "庭院设计", 3, "设计师", 0, "服务", true); err != nil || state.region != "福建省 泉州市 丰泽区" {
		t.Fatalf("old request cleared region=%q err=%v", state.region, err)
	}
	region := " 广东省  深圳市 南山区 "
	if err := m.UpdateDesignerProfile(7, "简介", "庭院设计", 3, "设计师", 0, "服务", true, &region); err != nil || state.region != "广东省 深圳市 南山区" {
		t.Fatalf("region update failed region=%q err=%v", state.region, err)
	}
	region = ""
	if err := m.UpdateDesignerProfile(7, "简介", "庭院设计", 3, "设计师", 0, "服务", true, &region); err != nil || state.region != "" {
		t.Fatalf("explicit clear failed region=%q err=%v", state.region, err)
	}
	region = strings.Repeat("中", 129)
	if err := m.UpdateDesignerProfile(7, "简介", "庭院设计", 3, "设计师", 0, "服务", true, &region); err == nil || state.writes != 3 {
		t.Fatalf("invalid region wrote profile writes=%d err=%v", state.writes, err)
	}
}
