package api

import (
	"testing"
)

// TestIsProtectedSasocHost 生产平台黑名单拦截：生成任务不得指向生产环境
func TestIsProtectedSasocHost(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"192.168.123.24", true},      // 生产（源 IP 摘录）
		{"192.168.123.24:4567", true}, // 生产带端口
		{"192.168.60.162", true},      // 生产（集团）
		{"192.168.60.162:4567", true},
		{"192.168.123.124", false}, // 测试平台（工具默认配置）
		{"192.168.123.124:4567", false},
		{"192.168.1.24", false}, // 相似但不匹配
		{"", false},
	}
	for _, c := range cases {
		if got := isProtectedSasocHost(c.host); got != c.want {
			t.Errorf("isProtectedSasocHost(%q) = %v, want %v", c.host, got, c.want)
		}
	}
}

// TestBuildSnPool SN 池生成：数量与格式符合协议（SN 为字符串介质标识）
func TestBuildSnPool(t *testing.T) {
	pool := buildSnPool("USB-", 5)
	if len(pool) != 5 {
		t.Fatalf("pool size = %d, want 5", len(pool))
	}
	expect := []string{"USB-000001", "USB-000002", "USB-000003", "USB-000004", "USB-000005"}
	for i, sn := range pool {
		if sn != expect[i] {
			t.Errorf("pool[%d] = %q, want %q", i, sn, expect[i])
		}
	}
}
