package service

import (
	"testing"

	"github.com/woodchen-ink/splitdns/server/model"
)

// planLine 是线路记录写入与巡检共用的对账: 认错记录会把别人的解析改掉, 说不清时必须拒绝,
// 这里守住筛选规则 (主机记录 / 线路 / 类型) 以及双栈下 "同类型一一对应、换类型复用旧记录" 的配对。
func TestPlanLine(t *testing.T) {
	rec := func(name, typ, line, value string) model.DNSRecord {
		return model.DNSRecord{Name: name, Type: typ, Line: line, Value: value, Enabled: true}
	}
	noise := []model.DNSRecord{
		rec("www", "A", "默认", "9.9.9.9"),
		rec("@", "TXT", "默认", "verify-token"),
		rec("@", "A", "境外", "8.8.8.8"),
	}
	with := func(rs ...model.DNSRecord) []model.DNSRecord { return append(append([]model.DNSRecord{}, noise...), rs...) }

	t.Run("空线路双栈全部新增", func(t *testing.T) {
		p := planLine(noise, "@", "默认", []string{"1.2.3.4", "2001:db8::1"})
		if len(p.Create) != 2 || len(p.Modify)+len(p.Delete)+len(p.Matched) != 0 {
			t.Fatalf("应新增 A + AAAA 且不碰别的记录, 实际 %+v", p)
		}
	})

	t.Run("老线路补 IPv6 不需要动现有记录", func(t *testing.T) {
		records := with(rec("@", "A", "默认", "1.2.3.4"))
		p := planLine(records, "@", "默认", []string{"1.2.3.4", "2001:db8::1"})
		if len(p.Matched) != 1 || len(p.Create) != 1 || p.Create[0].Type != "AAAA" || p.reshapes(records) {
			t.Fatalf("应只补一条 AAAA, 实际 %+v", p)
		}
	})

	t.Run("CNAME 换成双栈 IP 复用旧记录", func(t *testing.T) {
		records := with(rec("@", "CNAME", "默认", "rn.example.com"))
		p := planLine(records, "@", "默认", []string{"1.2.3.4", "2001:db8::1"})
		if len(p.Modify) != 1 || p.Modify[0].Want.Type != "A" || len(p.Create) != 1 || !p.reshapes(records) {
			t.Fatalf("CNAME 应改成 A、再补 AAAA, 且整条线路要等确认, 实际 %+v", p)
		}
	})

	t.Run("双栈换回 CNAME 删掉多出的那条", func(t *testing.T) {
		records := with(rec("@", "A", "默认", "1.2.3.4"), rec("@", "AAAA", "默认", "2001:db8::1"))
		p := planLine(records, "@", "默认", []string{"rn.example.com"})
		if len(p.Modify) != 1 || p.Modify[0].Want.Type != "CNAME" || len(p.Delete) != 1 || len(p.Create) != 0 {
			t.Fatalf("应把一条改成 CNAME、删掉另一条, 实际 %+v", p)
		}
	})

	t.Run("去掉 IPv6 后残留的 AAAA 要删", func(t *testing.T) {
		records := with(rec("@", "A", "默认", "1.2.3.4"), rec("@", "AAAA", "默认", "2001:db8::1"))
		p := planLine(records, "@", "默认", []string{"1.2.3.4"})
		if len(p.Matched) != 1 || len(p.Delete) != 1 || records[p.Delete[0]].Type != "AAAA" {
			t.Fatalf("应删掉 AAAA, 实际 %+v", p)
		}
	})

	t.Run("IPv6 不同写法与主机名大小写尾点都算等价", func(t *testing.T) {
		records := with(rec("@", "AAAA", "默认", "2001:db8::1"), rec("@", "CNAME", "境内", "Rn.Example.com."))
		if p := planLine(records, "@", "默认", []string{"2001:0db8:0:0::1"}); len(p.Matched) != 1 || len(p.Modify) != 0 {
			t.Fatalf("同一个 IPv6 地址不该待覆盖, 实际 %+v", p)
		}
		if p := planLine(records, "@", "境内", []string{"rn.example.com"}); len(p.Matched) != 1 {
			t.Fatalf("等价主机名不该待覆盖, 实际 %+v", p)
		}
	})

	t.Run("同线路同类型多条拒绝对账", func(t *testing.T) {
		records := with(rec("@", "A", "默认", "1.2.3.4"), rec("@", "A", "默认", "5.6.7.8"))
		if p := planLine(records, "@", "默认", []string{"1.2.3.4"}); p.Ambiguous == "" {
			t.Fatalf("同类型多条应判为说不清, 实际 %+v", p)
		}
	})
}

func TestValidateTargetValue(t *testing.T) {
	ok := []string{"", "rn.example.com", "1.2.3.4", "2001:db8::1", "1.2.3.4, 2001:db8::1", "2001:db8::1，1.2.3.4"}
	for _, v := range ok {
		if err := validateTargetValue(v); err != nil {
			t.Errorf("%q 应合法, 实际报错 %v", v, err)
		}
	}
	bad := []string{"1.2.3.4, 5.6.7.8", "2001:db8::1, 2001:db8::2", "1.2.3.4, rn.example.com"}
	for _, v := range bad {
		if err := validateTargetValue(v); err == nil {
			t.Errorf("%q 应被拒绝", v)
		}
	}
	if err := validateAddresses("1.2.3.4, origin.example.com"); err == nil {
		t.Error("源站 IP 里混了主机名应被拒绝")
	}
}

func TestRecordTypeFor(t *testing.T) {
	cases := map[string]string{
		"1.2.3.4": "A", "2001:db8::1": "AAAA", "::ffff:1.2.3.4": "AAAA",
		"rn.example.com": "CNAME", "1.2.3.4.example.com": "CNAME",
	}
	for v, want := range cases {
		if got := recordTypeFor(v); got != want {
			t.Errorf("recordTypeFor(%q) = %s, 期望 %s", v, got, want)
		}
	}
	if hostnameTarget("1.2.3.4, 2001:db8::1") {
		t.Error("双栈 IP 落点不是主机名, CF 里不需要记录")
	}
}
