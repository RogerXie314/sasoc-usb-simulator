package db

import (
	"strings"
	"testing"
)

// TestGenerateApplyCodeFormat 申领码格式：6位字母数字
func TestGenerateApplyCodeFormat(t *testing.T) {
	code := GenerateApplyCode()
	if len(code) != 6 {
		t.Fatalf("expected length 6, got %q len=%d", code, len(code))
	}
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	for _, ch := range code {
		if !strings.ContainsRune(charset, ch) {
			t.Fatalf("invalid char %q in code %q", ch, code)
		}
	}
}

// TestGenerateApplyCodeUnique 连续生成的申领码不应重复
func TestGenerateApplyCodeUnique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		code := GenerateApplyCode()
		if seen[code] {
			t.Fatalf("duplicate code generated: %s", code)
		}
		seen[code] = true
	}
}
