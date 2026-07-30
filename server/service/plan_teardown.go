package service

import (
	"fmt"
	"strings"

	"github.com/woodchen-ink/splitdns/server/model"
)

// teardownPrefix 是拆除步骤 Key 的统一前缀。
// 步骤按前缀分派而不是逐个值登记, 新增拆除步骤只加 case 不改分派结构。
const teardownPrefix = "teardown."

// buildTeardownSteps 生成拆除流程: 把配置流程在各平台留下的痕迹一处处撤掉。
//
// 顺序大体是配置流程的逆序, 但第一步固定撤委派 —— 反过来先删 DNSPod 域名的话,
// 父区的委派还指着一组不再托管这个域名的 NS, 解析器拿到的是 SERVFAIL,
// 比干脆查不到更难排查, 而且会一直重试。
//
// 每一步都可以跳过: 拆到一半改主意 (比如想留着 DNSPod 域名自己用) 是常见的,
// 流程不该逼着人把每一步都做完。
func buildTeardownSteps(h model.Hostname) []model.Step {
	var steps []model.Step
	seq := 0
	add := func(s model.Step) {
		seq++
		s.Seq = seq
		s.Status = model.StepPending
		s.Mode = model.StepManual
		s.Verifiable = true
		steps = append(steps, s)
	}

	useSaaS := h.SaaSZone != ""

	add(model.Step{
		Key:   "teardown.cf_delegation",
		Title: "撤掉父区里的 NS 委派",
		Instruction: fmt.Sprintf(
			"到 CF 的 %s 区, 删掉名称为 %s 的 NS 记录。\n"+
				"这一步一做完 %s 就不再解析了, 后面几步只是把各平台上的残留清干净。\n"+
				"先撤委派再动 DNSPod: 反过来的话委派还指着已经不托管这个域名的 NS, "+
				"解析器拿到的是 SERVFAIL 并且会一直重试, 比干脆查不到更糟。",
			h.ParentZone, relativeName(h.Hostname, h.ParentZone), h.Hostname),
		ETASeconds: 600,
	})

	add(model.Step{
		Key:         "teardown.dnspod_records",
		Title:       "删掉 DNSPod 上由本工具维护的解析记录",
		Instruction: "等这一步刷新出具体清单后再操作。只删区顶点的线路记录和证书验证 TXT, 其它记录一条不动 —— 那些可能是当初从父区搬过来的, 还在服务。",
		ETASeconds:  60,
	})

	add(model.Step{
		Key:   "teardown.dnspod_zone",
		Title: "从 DNSPod 删掉这个域名",
		Instruction: fmt.Sprintf(
			"到 DNSPod 把域名 %s 整个删掉。域名下如果还有别的记录会跟着一起没, 执行前的清单里会逐条列出来。\n"+
				"想留着这个域名自己接着用的话, 跳过这一步。",
			h.DNSPodZone()),
		ETASeconds: 30,
	})

	if useSaaS {
		add(model.Step{
			Key:   "teardown.saas_custom_hostname",
			Title: "删掉 SaaS 区的自定义主机名",
			Instruction: fmt.Sprintf(
				"到 CF 的 %s 区 → SSL/TLS → 自定义主机名, 删掉 %s。CF 会把它的证书一并吊销。\n"+
					"本工具当初为它建的那条落点记录会一并清掉; 但区里还有别的自定义主机名指着同一个源服务器、"+
					"或者它就是该区的回退源时不动 —— 删了会连累别人。",
				h.SaaSZone, h.Hostname),
			ETASeconds: 30,
		})
		add(model.Step{
			Key:   "teardown.saas_fallback",
			Title: "清掉 SaaS 区的回退源",
			Instruction: fmt.Sprintf(
				"回退源是 %s 这个区共享的设置, 区里只要还有别的自定义主机名就绝对不能动 —— 删了它们会一起失效。\n"+
					"程序会先数一遍, 只有确认区里已经没有别人才动手; 还有别人在用就跳过这一步。",
				h.SaaSZone),
			ETASeconds: 30,
		})
	}

	add(model.Step{
		Key:   "teardown.cf_leftovers",
		Title: "清掉父区里本工具写下的验证记录",
		Instruction: fmt.Sprintf(
			"配置时如果做过 DNSPod 的域名归属验证, %s 里会留着一条 TXT。它现在没有任何作用, 但会一直躺在记录列表里误导排查。\n"+
				"当初 DNSPod 要求把 TXT 加在别的主域名上的话, 那一条得自己去对应的区删 —— 这里只清父区。",
			h.ParentZone),
		ETASeconds: 30,
	})

	return steps
}

// refreshTeardownInstruction 把"现在实际还剩什么"填进拆除步骤的指令里。
// 拆除面对的是存量, 清单必须是当下的实际状态, 建流程时那份是拿不准的。
func refreshTeardownInstruction(step *model.Step, h model.Hostname, snap model.Snapshot) {
	switch step.Key {
	case "teardown.cf_delegation":
		if len(snap.Delegation) == 0 {
			return
		}
		step.Instruction = fmt.Sprintf(
			"到 CF 的 %s 区删掉名称为 %s 的这几条 NS 记录:\n  %s\n"+
				"删完 %s 立刻停止解析, 后面几步只是清残留。",
			h.ParentZone, relativeName(h.Hostname, h.ParentZone),
			strings.Join(snap.Delegation, "\n  "), h.Hostname)

	case "teardown.dnspod_records":
		mine, others := splitManagedRecords(snap.Records)
		if len(mine) == 0 && len(others) == 0 {
			return
		}
		var b strings.Builder
		fmt.Fprintf(&b, "DNSPod 的 %s 里, 这些记录是本工具建的, 会被删掉:\n", h.DNSPodZone())
		if len(mine) == 0 {
			b.WriteString("  (已经没有了)\n")
		}
		for _, r := range mine {
			fmt.Fprintf(&b, "  %s %s [%s] → %s\n", r.Name, r.Type, r.Line, r.Value)
		}
		if len(others) > 0 {
			fmt.Fprintf(&b, "另外这 %d 条不是本工具建的, 一条都不动:\n", len(others))
			for _, r := range others {
				fmt.Fprintf(&b, "  %s %s [%s] → %s\n", r.Name, r.Type, r.Line, r.Value)
			}
		}
		step.Instruction = b.String()

	case "teardown.dnspod_zone":
		if snap.DNSPodMissing {
			return
		}
		step.Instruction = fmt.Sprintf(
			"到 DNSPod 把域名 %s 整个删掉, 它下面现存的 %d 条记录会一起没。\n"+
				"想留着这个域名自己接着用的话, 跳过这一步。",
			h.DNSPodZone(), len(snap.Records))

	case "teardown.saas_fallback":
		if snap.FallbackOrigin == "" {
			return
		}
		step.Instruction = fmt.Sprintf(
			"%s 当前的回退源是 %s (状态 %s)。\n"+
				"这是整个区共享的设置, 区里只要还有别的自定义主机名就不能动 —— 删了它们会一起失效。\n"+
				"程序会先数一遍, 只有确认区里已经没有别人才动手; 还有别人在用就跳过这一步。",
			h.SaaSZone, snap.FallbackOrigin, orNone(snap.FallbackOriginStatus))

	case "teardown.cf_leftovers":
		if len(snap.ParentLeftovers) == 0 {
			return
		}
		step.Instruction = fmt.Sprintf(
			"到 CF 的 %s 区删掉这些由本工具写下的记录, 它们现在都没有作用了:\n  %s\n"+
				"当初 DNSPod 要求把归属验证 TXT 加在别的主域名上的话, 那一条得自己去对应的区删。",
			h.ParentZone, strings.Join(snap.ParentLeftovers, "\n  "))
	}
}

// teardownDeps 记录每个拆除步骤依赖哪一路巡检数据。
// 那一路没读到时不能判"已经清干净" —— 空快照和真的空长得一模一样。
var teardownDeps = map[string]string{
	"teardown.cf_delegation":        model.FetchParent,
	"teardown.dnspod_records":       model.FetchDNSPod,
	"teardown.dnspod_zone":          model.FetchDNSPod,
	"teardown.saas_custom_hostname": model.FetchSaaS,
	"teardown.saas_fallback":        model.FetchSaaS,
	"teardown.cf_leftovers":         model.FetchLeftovers,
}

// teardownSatisfied 判定某个拆除步骤要清的东西是不是真的没了。
// 判据一律是"巡检拉到的实际状态", 不是"接口调过了" —— 与配置流程的验证口径一致。
func teardownSatisfied(key string, snap model.Snapshot) (ok bool, known bool, reason string) {
	if source, dep := teardownDeps[key]; dep {
		if why, failed := snap.FetchErrors[source]; failed {
			return false, true, "这轮没能读到实际状态, 无法确认是否清干净: " + why
		}
	}

	switch key {
	case "teardown.cf_delegation":
		if len(snap.Delegation) == 0 {
			return true, true, ""
		}
		return false, true, fmt.Sprintf("父区还有 %d 条委派 NS: %s",
			len(snap.Delegation), strings.Join(snap.Delegation, ", "))

	case "teardown.dnspod_records":
		mine, _ := splitManagedRecords(snap.Records)
		if len(mine) == 0 {
			return true, true, ""
		}
		return false, true, fmt.Sprintf("DNSPod 上还有 %d 条本工具维护的记录", len(mine))

	case "teardown.dnspod_zone":
		if snap.DNSPodMissing {
			return true, true, ""
		}
		return false, true, "DNSPod 上还查得到这个域名"

	case "teardown.saas_custom_hostname":
		if !snap.CustomHostname.Exists {
			return true, true, ""
		}
		return false, true, fmt.Sprintf("SaaS 区里还有这个自定义主机名 (状态 %s)", orNone(snap.CustomHostname.Status))

	case "teardown.saas_fallback":
		if snap.FallbackOrigin == "" {
			return true, true, ""
		}
		return false, true, "SaaS 区的回退源还是 " + snap.FallbackOrigin

	case "teardown.cf_leftovers":
		if len(snap.ParentLeftovers) == 0 {
			return true, true, ""
		}
		return false, true, fmt.Sprintf("父区还有 %d 条本工具写下的记录", len(snap.ParentLeftovers))
	}
	return false, false, ""
}

// splitManagedRecords 把 DNSPod 上的记录分成"本工具维护的"和"别人的"。
// 拆除只碰前者: 后者可能是用户自己加的, 也可能是清理父区时搬过来的, 还在服务。
func splitManagedRecords(records []model.DNSRecord) (mine, others []model.DNSRecord) {
	for _, r := range records {
		if isManagedRecord(r.Name, r.Type) {
			mine = append(mine, r)
			continue
		}
		others = append(others, r)
	}
	return mine, others
}

// isManagedRecord 判定 DNSPod 上这条记录是不是配置流程自己建的。
// 两类: 区顶点上的线路落点, 以及证书 / 归属验证用的 TXT。
// 按位置和类型判定而不是记住建过什么 —— 用户中途手改过也能认出来。
func isManagedRecord(name, recordType string) bool {
	if name == "@" {
		return recordType == "A" || recordType == "AAAA" || recordType == "CNAME"
	}
	return recordType == "TXT" && isManagedTXT(name)
}
