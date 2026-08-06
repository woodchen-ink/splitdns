package service

import (
	"fmt"
	"sort"
	"strings"

	"github.com/woodchen-ink/splitdns/server/model"
)

// 默认线路名。DNSPod 上"默认"是兜底线路, 任何没命中其它线路的解析器都落到它,
// 缺了它会有一部分解析器拿不到任何记录。
const defaultLine = "默认"

// evaluate 汇总所有规则判定。规则按主题分组, 每组一个函数, 新增检查项只加函数不改这里的结构。
// 委派 / 父区相关的检查只在委派模式下跑 —— 直托模式没有父区, 缺委派记录不是故障是常态。
func evaluate(h model.Hostname, snap model.Snapshot) []model.Finding {
	var out []model.Finding
	if h.Delegated() {
		out = append(out, checkDelegation(snap)...)
		out = append(out, checkShadowed(snap)...)
	}
	out = append(out, checkNSPointed(h, snap)...)
	out = append(out, checkSaaS(h, snap)...)
	out = append(out, checkTXT(h, snap)...)
	out = append(out, checkRoutes(h, snap)...)
	out = append(out, checkZoneEnabled(snap)...)
	out = append(out, checkZoneExists(snap)...)
	return out
}

// checkNSPointed 直托模式下检查域名的 NS 是否指向 DNSPod —— 它是委派模式里 checkDelegation 的对应物。
// 判据是 DNSPod 自己的周期性检测: 只有它明确报 DNS_ERROR 才提醒, 空值不当"已确认正常";
// 检测可能滞后 (刚改完 NS 要等它重测), 所以级别用 warn, 不把步骤卡死在别人的缓存上。
func checkNSPointed(h model.Hostname, snap model.Snapshot) []model.Finding {
	if h.Delegated() || !strings.EqualFold(snap.DNSPodDNSStatus, "DNS_ERROR") {
		return nil
	}
	return []model.Finding{{
		Level:  model.LevelWarn,
		Code:   "dnspod.ns_unpointed",
		Title:  "DNSPod 检测到域名的 NS 没有指向它",
		Detail: fmt.Sprintf("%s 的 DNS 检测状态是 DNS_ERROR, NS 不对时记录配得再对也不生效", h.DNSPodZone()),
		Fix: "到域名注册商处把 NS 改成 DNSPod 分配的那组 (" + strings.Join(snap.DNSPodNameservers, ", ") +
			")。刚改过的话等 DNSPod 重新检测即可",
	}}
}

// checkZoneExists 报告域名压根不在 DNSPod 上。
// 与"读取失败"分开报: 前者照着流程加一下就行, 后者是凭据 / 网络的问题, 处理方式完全不同。
func checkZoneExists(snap model.Snapshot) []model.Finding {
	if !snap.DNSPodMissing {
		return nil
	}
	return []model.Finding{{
		Level: model.LevelError,
		Code:  "dnspod.missing",
		Title: "DNSPod 上查不到这个域名",
		Fix:   "在配置流程的「在 DNSPod 添加域名」那一步点自动执行",
	}}
}

// checkDelegation 校验 CF 父区的 NS 委派是否存在, 且与 DNSPod 实际分配的 NS 一致。
// 委派指向一组过期的 NS 是很隐蔽的故障: 面板上看着有记录, 解析却落不到 DNSPod。
func checkDelegation(snap model.Snapshot) []model.Finding {
	if len(snap.Delegation) == 0 {
		return []model.Finding{{
			Level: model.LevelError,
			Code:  "delegation.missing",
			Title: "父区没有 NS 委派记录",
			Fix:   "在 CF 父区给该子域名加 NS 记录, 指向 DNSPod 分配的那组 NS",
		}}
	}
	if len(snap.DNSPodNameservers) == 0 {
		return nil
	}

	want := normalizeSet(snap.DNSPodNameservers)
	got := normalizeSet(snap.Delegation)
	if !equalSet(want, got) {
		return []model.Finding{{
			Level:  model.LevelError,
			Code:   "delegation.mismatch",
			Title:  "委派的 NS 与 DNSPod 分配的不一致",
			Detail: fmt.Sprintf("父区: %s / DNSPod: %s", strings.Join(got, ", "), strings.Join(want, ", ")),
			Fix:    "以 DNSPod 控制台显示的 NS 为准, 改掉父区的 NS 记录",
		}}
	}
	return nil
}

// checkZoneEnabled 检查 DNSPod 上该域名的解析是否启用。
// 新加的域名默认是暂停状态, 这时记录全对、委派也对, 解析就是不出结果 —— 很难靠肉眼发现。
func checkZoneEnabled(snap model.Snapshot) []model.Finding {
	if len(snap.DNSPodNameservers) == 0 || snap.DNSPodEnabled {
		return nil
	}
	return []model.Finding{{
		Level:  model.LevelError,
		Code:   "dnspod.paused",
		Title:  "DNSPod 上这个域名的解析没有对外生效",
		Detail: "DNSPod 返回的状态是 " + orNone(snap.DNSPodStatus),
		Fix:    "记录配得再对也不会生效。在「在 DNSPod 添加域名」那一步点自动执行即可开启",
	}}
}

// checkShadowed 报告被委派遮蔽的记录。这些记录在 CF 面板里仍然可见, 但一条都不生效。
func checkShadowed(snap model.Snapshot) []model.Finding {
	if len(snap.ShadowedRecords) == 0 {
		return nil
	}
	return []model.Finding{{
		Level:  model.LevelWarn,
		Code:   "shadowed.records",
		Title:  fmt.Sprintf("父区有 %d 条记录被委派遮蔽", len(snap.ShadowedRecords)),
		Detail: strings.Join(snap.ShadowedRecords, "; "),
		Fix:    "这些记录不生效但会误导排查, 建议从 CF 父区删掉",
	}}
}

// checkRoutes 校验每条配置的线路在 DNSPod 上是否真的落地且值正确, 并检查兜底线路与配置漂移。
func checkRoutes(h model.Hostname, snap model.Snapshot) []model.Finding {
	var out []model.Finding
	recordName := routeRecordName(h)
	apex := routeRecords(snap.Records, recordName)

	hasDefault := false
	declared := map[string]bool{}

	for _, route := range h.Routes {
		if route.Line == defaultLine {
			hasDefault = true
		}
		declared[route.Line] = true

		want, err := expectedValue(route, snap)
		if err != nil {
			out = append(out, model.Finding{
				Level:  model.LevelError,
				Code:   "route.origin_unresolved",
				Title:  fmt.Sprintf("线路「%s」的回源无法解析", route.Line),
				Detail: err.Error(),
				Fix:    "检查该线路绑定的回源配置",
			})
			continue
		}

		rec, ok := apex[route.Line]
		if !ok {
			out = append(out, model.Finding{
				Level:  model.LevelError,
				Code:   "route.missing",
				Title:  fmt.Sprintf("线路「%s」在 DNSPod 上没有记录", route.Line),
				Detail: fmt.Sprintf("期望指向 %s", want),
				Fix:    fmt.Sprintf("在 DNSPod 的 %s 里给主机记录 %s 加一条「%s」线路记录", h.DNSPodZone(), recordName, route.Line),
			})
			continue
		}
		if !sameName(rec.Value, want) {
			out = append(out, model.Finding{
				Level:  model.LevelError,
				Code:   "route.mismatch",
				Title:  fmt.Sprintf("线路「%s」指向的目标与配置不符", route.Line),
				Detail: fmt.Sprintf("实际 %s / 期望 %s", rec.Value, want),
				Fix:    "在「配置分线路解析记录」那一步让程序覆盖 (会先列出差异等确认), 或把这里的回源配置改成实际值",
			})
		}
		if !rec.Enabled {
			out = append(out, model.Finding{
				Level: model.LevelError,
				Code:  "route.disabled",
				Title: fmt.Sprintf("线路「%s」的记录处于停用状态", route.Line),
				Fix:   "在 DNSPod 里启用该记录",
			})
		}
	}

	if len(h.Routes) > 0 && !hasDefault {
		out = append(out, model.Finding{
			Level: model.LevelWarn,
			Code:  "route.no_default",
			Title: "没有配置兜底的「默认」线路",
			Fix:   "把覆盖面最广的那条线路改挂到「默认」, 否则识别不出归属的解析器拿不到记录",
		})
	}

	// 反向对账: DNSPod 上有、配置里没声明的线路, 说明有人绕过工具直接改了解析
	for line := range apex {
		if !declared[line] {
			out = append(out, model.Finding{
				Level:  model.LevelWarn,
				Code:   "route.undeclared",
				Title:  fmt.Sprintf("DNSPod 上的线路「%s」没有在这里登记", line),
				Detail: fmt.Sprintf("实际指向 %s", apex[line].Value),
				Fix:    "补一条线路配置, 或确认这条解析是否该删",
			})
		}
	}
	return out
}

// expectedValue 算出某条线路应该解析到的目标值。
// 回源类型是开放式取值: SaaS 回退源要到运行时才知道具体主机名, 其余类型一律取配置里的落点值,
// 新增回源类型不需要在这里加分支。
func expectedValue(route model.Route, snap model.Snapshot) (string, error) {
	target := route.Target()
	if target == nil {
		return "", fmt.Errorf("线路既没引用回源, 也没填落点值")
	}
	// 走 CF for SaaS 的线路: CNAME 指向 SaaS 区里任意一条橙云记录即可, 不必非得是被设为「回退源」的那条 ——
	// 流量到了 CF 边缘是按 Host 头找自定义主机名的, CNAME 目标只负责把流量带进这个区。
	// 所以填了落点值就以它为准, 没填才回落到该区当前的回退源。
	if target.Kind == model.OriginSaaSFallback || target.Kind == model.OriginSaaSCustom {
		if target.Value != "" {
			return target.Value, nil
		}
		if snap.FallbackOrigin == "" {
			return "", fmt.Errorf("这条线路没填落点值, 而 SaaS 区也还没设置回退源")
		}
		return snap.FallbackOrigin, nil
	}
	if target.Value == "" {
		return "", fmt.Errorf("回源 %s 没有填落点值", target.Name)
	}
	return target.Value, nil
}

// routeRecordName 返回该域名的线路记录在 DNSPod 里的主机记录名。
// 委派模式下访问域名就是 DNSPod 区顶点, 得到 "@"; 直托模式下访问域名是根域名 (也是 "@")
// 或它的子域 (得到相对名, 如 "www")。线路记录的读写与判定都用这一个结果。
func routeRecordName(h model.Hostname) string {
	return relativeName(h.Hostname, h.DNSPodZone())
}

// routeRecords 收集指定主机记录名上按线路索引的落点记录 (A / AAAA / CNAME)。
func routeRecords(records []model.DNSRecord, name string) map[string]model.DNSRecord {
	out := make(map[string]model.DNSRecord)
	for _, r := range records {
		if !sameName(r.Name, name) {
			continue
		}
		if r.Type != "CNAME" && r.Type != "A" && r.Type != "AAAA" {
			continue
		}
		out[r.Line] = r
	}
	return out
}

func normalizeSet(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		out = append(out, normalizeName(s))
	}
	sort.Strings(out)
	return out
}

func equalSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
