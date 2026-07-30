package service

import (
	"testing"

	"github.com/woodchen-ink/splitdns/server/model"
)

// 两种接入模式下的步骤生成与记录认领。
// 直托模式的红线在这里守着: 不许出现"删 DNSPod 域名"这一步,
// 也不许把用户自己的记录认成"本工具建的"。

func stepKeys(steps []model.Step) []string {
	out := make([]string, 0, len(steps))
	for _, s := range steps {
		out = append(out, s.Key)
	}
	return out
}

func hasKey(steps []model.Step, key string) bool {
	for _, s := range steps {
		if s.Key == key {
			return true
		}
	}
	return false
}

func delegatedHost() model.Hostname {
	return model.Hostname{
		Hostname:   "img.example.com",
		ParentZone: "example.com",
		SaaSZone:   "saas.example.net",
		Routes: []model.Route{
			{Line: "默认", Kind: model.OriginSaaSFallback, Value: "cf.example.net"},
			{Line: "境内", Kind: model.OriginCNAME, Value: "img.example.com.eo.dnse2.com"},
		},
	}
}

func directHost() model.Hostname {
	return model.Hostname{
		Hostname:     "example.com",
		DNSPodDomain: "example.com",
		SaaSZone:     "saas.example.net",
		Routes: []model.Route{
			{Line: "默认", Kind: model.OriginSaaSFallback, Value: "cf.example.net"},
			{Line: "境内", Kind: model.OriginSaaSFallback, Value: "youxuan.example.org"},
		},
	}
}

func TestBuildStepsByMode(t *testing.T) {
	delegated := buildSteps(delegatedHost())
	for _, key := range []string{"cf.cleanup", "cf.delegation", "dnspod.zone", "dnspod.routes", "saas.custom_hostname"} {
		if !hasKey(delegated, key) {
			t.Errorf("委派模式缺少步骤 %s, 实际: %v", key, stepKeys(delegated))
		}
	}

	direct := buildSteps(directHost())
	for _, key := range []string{"cf.cleanup", "cf.delegation"} {
		if hasKey(direct, key) {
			t.Errorf("直托模式不该有父区步骤 %s, 实际: %v", key, stepKeys(direct))
		}
	}
	for _, key := range []string{"dnspod.zone", "dnspod.routes", "saas.custom_hostname", "saas.cert"} {
		if !hasKey(direct, key) {
			t.Errorf("直托模式缺少步骤 %s, 实际: %v", key, stepKeys(direct))
		}
	}
}

func TestBuildTeardownStepsByMode(t *testing.T) {
	delegated := buildTeardownSteps(delegatedHost())
	for _, key := range []string{"teardown.cf_delegation", "teardown.dnspod_zone", "teardown.cf_leftovers"} {
		if !hasKey(delegated, key) {
			t.Errorf("委派模式拆除缺少步骤 %s, 实际: %v", key, stepKeys(delegated))
		}
	}
	if delegated[0].Key != "teardown.cf_delegation" {
		t.Errorf("委派模式拆除第一步必须是撤委派, 实际: %v", stepKeys(delegated))
	}

	direct := buildTeardownSteps(directHost())
	// 删 DNSPod 域名等于端掉用户根域名的整个 DNS, 这一步在直托模式连出现的机会都不能有
	for _, key := range []string{"teardown.cf_delegation", "teardown.dnspod_zone", "teardown.cf_leftovers"} {
		if hasKey(direct, key) {
			t.Errorf("直托模式拆除不该有步骤 %s, 实际: %v", key, stepKeys(direct))
		}
	}
	if direct[0].Key != "teardown.dnspod_records" {
		t.Errorf("直托模式拆除第一步应是删线路记录 (停止解析的那一刀), 实际: %v", stepKeys(direct))
	}
}

func TestRouteRecordName(t *testing.T) {
	cases := []struct {
		name string
		h    model.Hostname
		want string
	}{
		{"委派模式访问域名即区顶点", delegatedHost(), "@"},
		{"直托模式根域名", directHost(), "@"},
		{"直托模式子域名", model.Hostname{Hostname: "www.example.com", DNSPodDomain: "example.com"}, "www"},
	}
	for _, c := range cases {
		if got := routeRecordName(c.h); got != c.want {
			t.Errorf("%s: routeRecordName = %q, 期望 %q", c.name, got, c.want)
		}
	}
}

func TestIsManagedRecord(t *testing.T) {
	direct := directHost()
	directSub := model.Hostname{
		Hostname:     "www.example.com",
		DNSPodDomain: "example.com",
		Routes:       []model.Route{{Line: "默认", Kind: model.OriginSaaSFallback, Value: "cf.example.net"}},
	}
	delegated := delegatedHost()

	cases := []struct {
		desc                    string
		h                       model.Hostname
		name, line, recordType  string
		want                    bool
	}{
		{"声明过的线路落点算我的", direct, "@", "境内", "CNAME", true},
		{"没声明过的线路是用户自己的解析", direct, "@", "联通", "A", false},
		{"区顶点的 MX 与落点无关", direct, "@", "默认", "MX", false},
		{"NS 记录永远不动", direct, "@", "默认", "NS", false},
		{"本工具写的 DCV TXT", direct, "_acme-challenge", "默认", "TXT", true},
		{"本工具写的归属 TXT", direct, "_cf-custom-hostname", "默认", "TXT", true},
		{"用户别的证书的 DCV 不能连坐", direct, "_acme-challenge.mail", "默认", "TXT", false},
		{"子域直托: 自己主机记录上的落点", directSub, "www", "默认", "CNAME", true},
		{"子域直托: 区顶点是别人的", directSub, "@", "默认", "CNAME", false},
		{"子域直托: 对应的 DCV TXT", directSub, "_acme-challenge.www", "默认", "TXT", true},
		{"子域直托: 区顶点的 DCV 是别人的", directSub, "_acme-challenge", "默认", "TXT", false},
		{"委派模式: 声明线路的落点照旧算我的", delegated, "@", "默认", "CNAME", true},
		{"委派模式: 没声明的线路也不再认领", delegated, "@", "境外", "A", false},
		{"委派模式: DCV TXT 照旧", delegated, "_acme-challenge", "默认", "TXT", true},
	}
	for _, c := range cases {
		if got := isManagedRecord(c.h, c.name, c.line, c.recordType); got != c.want {
			t.Errorf("%s: isManagedRecord(%q, %q, %q) = %v, 期望 %v", c.desc, c.name, c.line, c.recordType, got, c.want)
		}
	}
}

func TestEvaluateSkipsDelegationChecksInDirectMode(t *testing.T) {
	snap := model.Snapshot{DNSPodNameservers: []string{"a.dnspod.net"}, DNSPodEnabled: true}

	for _, f := range evaluate(directHost(), snap) {
		if f.Code == "delegation.missing" || f.Code == "delegation.mismatch" {
			t.Errorf("直托模式不该报委派问题, 却报了 %s", f.Code)
		}
	}

	found := false
	for _, f := range evaluate(delegatedHost(), snap) {
		if f.Code == "delegation.missing" {
			found = true
		}
	}
	if !found {
		t.Errorf("委派模式没有委派记录时应报 delegation.missing")
	}
}

func TestCheckNSPointed(t *testing.T) {
	bad := model.Snapshot{DNSPodDNSStatus: "DNS_ERROR", DNSPodNameservers: []string{"a.dnspod.net"}}
	if got := checkNSPointed(directHost(), bad); len(got) != 1 || got[0].Code != "dnspod.ns_unpointed" {
		t.Errorf("直托模式 DNS_ERROR 应报 ns_unpointed, 实际 %v", got)
	}
	// 委派模式有 checkDelegation 做权威判定, 这里不重复报
	if got := checkNSPointed(delegatedHost(), bad); len(got) != 0 {
		t.Errorf("委派模式不该报 ns_unpointed, 实际 %v", got)
	}
	// 空值只代表"没报错", 不下结论
	if got := checkNSPointed(directHost(), model.Snapshot{}); len(got) != 0 {
		t.Errorf("DNS 状态为空时不该报, 实际 %v", got)
	}
}
