package model

import "testing"

func TestAIContentAuditSampled(t *testing.T) {
	cases := []struct {
		name    string
		enabled bool
		rate    float64
		roll    float64
		want    bool
	}{
		{"开关关闭不抽", false, 0.02, 0.001, false},
		{"比例为0不抽", true, 0, 0.001, false},
		{"低于比例命中", true, 0.02, 0.015, true},
		{"等于比例不中（开区间上界）", true, 0.02, 0.02, false},
		{"高于比例不中", true, 0.02, 0.03, false},
		{"比例1必中", true, 1, 0.999, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AIContentAuditSampled(tc.enabled, tc.rate, tc.roll); got != tc.want {
				t.Errorf("AIContentAuditSampled(%v, %v, %v) = %v, want %v", tc.enabled, tc.rate, tc.roll, got, tc.want)
			}
		})
	}
}
