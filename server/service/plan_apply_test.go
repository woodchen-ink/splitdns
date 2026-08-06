package service

import (
	"testing"

	"github.com/woodchen-ink/splitdns/server/pkg/dnspod"
)

// overwriteTargets 是覆盖前的定位判定: 认错记录会把别人的解析改掉, 命中多条必须拒绝执行,
// 这里守住筛选规则 —— 主机记录 / 线路 / 类型都要匹配, 值等价的不算待覆盖。
func TestOverwriteTargets(t *testing.T) {
	records := []dnspod.Record{
		{ID: 1, Name: "@", Type: "CNAME", Line: "默认", Value: "rs2000.example.com."},
		{ID: 2, Name: "@", Type: "CNAME", Line: "境外", Value: "other.example.com"},
		{ID: 3, Name: "www", Type: "CNAME", Line: "默认", Value: "rs2000.example.com"},
		{ID: 4, Name: "@", Type: "TXT", Line: "默认", Value: "verify-token"},
		{ID: 5, Name: "@", Type: "CNAME", Line: "境内", Value: "Rn-22dc3.Example.com."},
	}

	got := overwriteTargets(records, "@", "默认", "rn-22dc3.example.com")
	if len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("默认线应只命中 ID 1 (线路 / 主机记录 / 类型不符的都要滤掉), 实际 %v", got)
	}

	// 大小写与尾点差异是等价值, 不该进覆盖清单
	if got := overwriteTargets(records, "@", "境内", "rn-22dc3.example.com"); len(got) != 0 {
		t.Fatalf("值已等价的记录不该待覆盖, 实际 %v", got)
	}

	// 同线路多条都不符 (如用户自己的双栈 A + AAAA) 时全部命中, 由调用方拒绝执行
	dual := []dnspod.Record{
		{ID: 6, Name: "@", Type: "A", Line: "默认", Value: "1.2.3.4"},
		{ID: 7, Name: "@", Type: "AAAA", Line: "默认", Value: "2001:db8::1"},
	}
	if got := overwriteTargets(dual, "@", "默认", "rn-22dc3.example.com"); len(got) != 2 {
		t.Fatalf("同线路多条不符应全部命中, 实际 %v", got)
	}
}
