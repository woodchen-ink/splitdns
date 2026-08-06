package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/woodchen-ink/go-web-utils/timex"
	"github.com/woodchen-ink/splitdns/server/database"
	"github.com/woodchen-ink/splitdns/server/model"
	"gorm.io/gorm"
)

// PlanView 是流程页需要的全部数据: 步骤状态 + 该域名的实时巡检结果。
// 一次请求给全, 前端不用再调第二个接口自己拼。
type PlanView struct {
	Plan     model.Plan           `json:"plan"`
	Hostname model.Hostname       `json:"hostname"`
	Report   model.HostnameReport `json:"report"`
}

// RefreshPlan 重新巡检一次并据此刷新所有步骤状态。
// 只巡检一次、所有步骤共用同一份快照: 逐步各查一次 API 既慢, 又可能读到互相不一致的中间态。
func RefreshPlan(ctx context.Context, planID uint) (*PlanView, error) {
	plan, err := loadPlan(planID)
	if err != nil {
		return nil, err
	}
	h, err := HostnameByID(plan.HostnameID)
	if err != nil {
		return nil, err
	}

	report := Inspect(ctx, *h)
	now := timex.Now()
	allDone := true

	for i := range plan.Steps {
		step := &plan.Steps[i]

		// 跳过是终态: 用户明确说了这一步不做 (比如回退源还有别人在用),
		// 再去验证只会把它翻回未完成, 也不该拦着整条流程收尾
		if step.Status == model.StepSkipped {
			continue
		}
		refreshInstruction(step, *h, report.Snapshot)

		if !step.Verifiable {
			// 程序验不了的步骤 (如源站 SNI 路由) 只认用户自己点的确认
			if step.Status != model.StepDone {
				allDone = false
			}
			continue
		}

		ok, known, reason := stepSatisfied(step.Key, *h, report.Snapshot)
		step.LastCheckedAt = &now
		switch {
		case !known:
			// 未登记的步骤标记为不可自动验证, 交回人工确认, 不静默当成通过
			step.Verifiable = false
			step.LastError = "该步骤没有对应的自动验证规则, 请手动确认"
			allDone = false
		case ok:
			if step.Status != model.StepDone {
				step.Status = model.StepDone
				step.DoneAt = &now
			}
			step.LastError = ""
		default:
			// 曾经通过又变回不通过, 说明线上被改动了, 状态要跟着退回去
			step.Status = model.StepWaiting
			step.DoneAt = nil
			step.LastError = reason
			allDone = false
		}

		if err := database.DB.Save(step).Error; err != nil {
			return nil, fmt.Errorf("保存步骤状态失败: %w", err)
		}
	}

	if allDone && plan.Status != model.PlanDone {
		plan.Status = model.PlanDone
		if err := database.DB.Model(plan).Update("status", model.PlanDone).Error; err != nil {
			return nil, err
		}
	}

	return &PlanView{Plan: *plan, Hostname: *h, Report: report}, nil
}

// loadPlan 读流程及其步骤, 步骤按展示顺序排好。
func loadPlan(planID uint) (*model.Plan, error) {
	var plan model.Plan
	err := database.DB.
		Preload("Steps", func(db *gorm.DB) *gorm.DB { return db.Order("seq") }).
		First(&plan, planID).Error
	if err != nil {
		return nil, fmt.Errorf("读取流程 %d 失败: %w", planID, err)
	}
	return &plan, nil
}

// refreshInstruction 把只有运行时才知道的值 (CF 给的 TXT、DNSPod 分配的 NS、各线路目标)
// 填进步骤指令里。这些值在建流程时还不存在, 必须每次巡检后重算。
func refreshInstruction(step *model.Step, h model.Hostname, snap model.Snapshot) {
	if strings.HasPrefix(step.Key, teardownPrefix) {
		refreshTeardownInstruction(step, h, snap)
		return
	}
	zone := h.DNSPodZone()

	switch step.Key {
	case "dnspod.dcv_txt":
		missing := missingTXT(h, snap)
		if len(missing) == 0 {
			return
		}
		var b strings.Builder
		fmt.Fprintf(&b, "到 DNSPod 的 %s 里补这些 TXT 记录 (线路选「默认」):\n", zone)
		for _, req := range missing {
			fmt.Fprintf(&b, "  主机记录 %s   值 %s\n", relativeName(req.Name, zone), req.Value)
		}
		b.WriteString("同名多条是正常的, 证书带通配符 SAN 时基础域名和通配符各要一条, 少一条证书就签不出来。")
		step.Instruction = b.String()

	case "dnspod.routes":
		var b strings.Builder
		fmt.Fprintf(&b, "到 DNSPod 的 %s 里, 给主机记录 %s 按线路加记录:\n", zone, routeRecordName(h))
		for _, route := range h.Routes {
			want, err := expectedValue(route, snap)
			if err != nil {
				fmt.Fprintf(&b, "  线路 %s   ⚠ %s\n", route.Line, err.Error())
				continue
			}
			fmt.Fprintf(&b, "  线路 %s   %s   值 %s\n", route.Line, recordTypeFor(want), want)
		}
		b.WriteString("TTL 用 600 秒(DNSPod 免费版最低)。CF 那条务必挂在「默认」线兜底, 只配境内+境外会让识别不出归属的解析器拿不到记录。")
		step.Instruction = b.String()

	case "cf.delegation":
		if len(snap.DNSPodNameservers) == 0 {
			return
		}
		step.Instruction = fmt.Sprintf(
			"在 CF 的 %s 区加 NS 记录, 名称填 %s, 内容分别填:\n  %s\n"+
				"代理状态显示「仅 DNS」是正常的, NS 类型没有橙云开关。\n"+
				"这一步只影响这一个名字, 父区其它记录和注册商那边都不用动。",
			h.ParentZone,
			relativeName(h.Hostname, h.ParentZone),
			strings.Join(snap.DNSPodNameservers, "\n  "))

	case "cf.cleanup":
		if len(snap.ShadowedRecords) == 0 {
			return
		}
		step.Instruction = fmt.Sprintf(
			"到 CF 的 %s 区删掉这些记录, 它们已经被委派遮蔽、一条都不生效:\n  %s",
			h.ParentZone, strings.Join(snap.ShadowedRecords, "\n  "))

	case "origin.sni_route":
		step.Instruction = sniRouteInstruction(h, snap)
	}
}

// sniRouteInstruction 生成 SNI 路由那一步的指令。
// 这步程序验不了, 所以指令必须具体到"哪台机器、加哪个名字、怎么自检",
// 光说"给 SNI 挂个 router"等于把活儿又扔回给人。
func sniRouteInstruction(h model.Hostname, snap model.Snapshot) string {
	// CF 上实际生效的值优先, 没有再退回配置值 —— 前者才是回源时真正握手用的名字
	sni := snap.CustomHostname.CustomOriginSNI
	origin := snap.CustomHostname.CustomOrigin
	if origin == "" {
		if o := findOrigin(h, model.OriginSaaSCustom); o != nil {
			origin = o.Value
			sni = o.SNI
		}
	}
	if sni == "" {
		sni = origin
	}
	if origin == "" {
		return "这个域名当前没有自定义源服务器, 这一步可以直接确认完成。"
	}

	addr := snap.CustomHostname.CustomOriginRecord.Content
	where := fmt.Sprintf("%s 指向的那台机器", origin)
	if addr != "" {
		where = fmt.Sprintf("%s (%s) 那台机器", origin, addr)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "CF 回源到自定义源服务器时, Host 头是 %s, 但 TLS 握手用的 SNI 是 %s。\n", h.Hostname, sni)
	b.WriteString("源站上的 Traefik / Nginx 按名字路由, 匹配不到就直接 403 —— 跟证书、跟后端服务都没关系。\n\n")
	fmt.Fprintf(&b, "要做的事: 到 %s 上, 给 %s 这个名字加一条路由\n", where, sni)
	fmt.Fprintf(&b, "  Traefik: 加一个 rule 为 Host(`%s`) 的 router, service 指向哪个都行\n", sni)
	fmt.Fprintf(&b, "  Nginx:   加一个 server_name 为 %s 的 server 块\n", sni)
	b.WriteString("指向哪个服务、有没有证书都无所谓, 只要它认得这个名字。\n\n")

	b.WriteString("加完自检 (返回不是 403 就算通了):\n")
	if addr != "" {
		fmt.Fprintf(&b, "  curl -sI --resolve %s:443:%s https://%s/\n", sni, addr, sni)
	} else {
		fmt.Fprintf(&b, "  curl -sI https://%s/\n", sni)
	}
	b.WriteString("\n这一步程序无法自动验证, 确认没问题后点「确认完成」。")
	return b.String()
}

// recordTypeFor 按落点值形态推断该建什么类型的记录: 像 IP 就是 A/AAAA, 否则 CNAME。
func recordTypeFor(value string) string {
	if strings.Count(value, ":") >= 2 {
		return "AAAA"
	}
	if isIPv4(value) {
		return "A"
	}
	return "CNAME"
}

// isIPv4 判断字符串是否是点分十进制 IPv4。
func isIPv4(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		if p == "" || len(p) > 3 {
			return false
		}
		for _, ch := range p {
			if ch < '0' || ch > '9' {
				return false
			}
		}
	}
	return true
}

// stepSatisfied 判定某一步是否已经在真实环境里生效。
// Key 未登记时返回不可判定, 由调用方保持原状并提示人工确认, 不静默当成通过。
func stepSatisfied(key string, h model.Hostname, snap model.Snapshot) (ok bool, known bool, reason string) {
	if strings.HasPrefix(key, teardownPrefix) {
		return teardownSatisfied(key, h, snap)
	}
	switch key {
	case "saas.fallback_origin":
		if snap.FallbackOriginStatus != "active" {
			return false, true, fmt.Sprintf("回退源状态为 %q, 需要 active", orNone(snap.FallbackOriginStatus))
		}
		// 状态 active 说明这一路读到了, 值不符就是真漂移, 不会把拉取失败当成退回理由
		if want := fallbackOriginTarget(h); want != nil && want.Value != "" && !sameName(snap.FallbackOrigin, want.Value) {
			return false, true, fmt.Sprintf("回退源还是 %s, 期望 %s", orNone(snap.FallbackOrigin), want.Value)
		}
		return true, true, ""

	case "saas.custom_hostname":
		if !snap.CustomHostname.Exists {
			return false, true, "SaaS 区里还没有这个自定义主机名"
		}
		// 主机名在了不算完: 源服务器字段配在它身上, 与声明的「自定义源」漂移时这一步必须退回 ——
		// 步骤一直显示完成的话, 前端不渲染执行按钮, 巡检报的错就没有任何入口能修
		if reasons := errorReasons(checkCustomOrigin(h, snap.CustomHostname)); len(reasons) > 0 {
			return false, true, strings.Join(reasons, "; ")
		}
		return true, true, ""

	case "dnspod.zone":
		if len(snap.DNSPodNameservers) == 0 {
			return false, true, "DNSPod 上还查不到这个域名"
		}
		if !snap.DNSPodEnabled {
			return false, true, "域名加好了, 但解析没有对外生效 (DNSPod 状态: " + orNone(snap.DNSPodStatus) + ")"
		}
		return true, true, ""

	case "dnspod.dcv_txt":
		missing := missingTXT(h, snap)
		if len(missing) == 0 {
			return true, true, ""
		}
		return false, true, fmt.Sprintf("还缺 %d 条验证 TXT", len(missing))

	case "dnspod.routes":
		if reasons := errorReasons(checkRoutes(h, snap)); len(reasons) > 0 {
			return false, true, strings.Join(reasons, "; ")
		}
		return true, true, ""

	case "cf.cleanup":
		if len(snap.ShadowedRecords) == 0 {
			return true, true, ""
		}
		return false, true, fmt.Sprintf("父区还有 %d 条记录被遮蔽", len(snap.ShadowedRecords))

	case "cf.delegation":
		if reasons := errorReasons(checkDelegation(snap)); len(reasons) > 0 {
			return false, true, strings.Join(reasons, "; ")
		}
		return true, true, ""

	case "saas.cert":
		if snap.CustomHostname.Status == "active" && snap.CustomHostname.SSLStatus == "active" {
			return true, true, ""
		}
		return false, true, fmt.Sprintf("主机名状态 %q / 证书状态 %q",
			orNone(snap.CustomHostname.Status), orNone(snap.CustomHostname.SSLStatus))
	}
	return false, false, ""
}

// missingTXT 返回 CF 要求、但 DNSPod 上还没落地的验证 TXT。
// 归属验证一条 + DCV 若干条; 证书带通配符 SAN 时 DCV 会是多条同名不同值, 必须逐条比对值而不是只看名字存在。
func missingTXT(h model.Hostname, snap model.Snapshot) []model.TXTRequirement {
	var want []model.TXTRequirement
	if snap.CustomHostname.OwnershipTXT.Name != "" {
		want = append(want, snap.CustomHostname.OwnershipTXT)
	}
	want = append(want, snap.CustomHostname.DCVTXT...)

	zone := h.DNSPodZone()
	var missing []model.TXTRequirement
	for _, req := range want {
		if !txtPresent(snap.Records, relativeName(req.Name, zone), req.Value) {
			missing = append(missing, req)
		}
	}
	return missing
}

// txtPresent 判断某条 TXT 是否已存在。DNSPod 侧可能带引号存储, 比较前统一剥掉。
func txtPresent(records []model.DNSRecord, name, value string) bool {
	for _, r := range records {
		if r.Type != "TXT" || !sameName(r.Name, name) {
			continue
		}
		if strings.Trim(r.Value, `"`) == strings.Trim(value, `"`) {
			return true
		}
	}
	return false
}

// errorReasons 从判定结果里挑出 error 级别的原因, warn 不阻塞步骤完成。
func errorReasons(findings []model.Finding) []string {
	var out []string
	for _, f := range findings {
		if f.Level != model.LevelError {
			continue
		}
		if f.Detail != "" {
			out = append(out, f.Title+": "+f.Detail)
			continue
		}
		out = append(out, f.Title)
	}
	return out
}

func orNone(s string) string {
	if s == "" {
		return "(空)"
	}
	return s
}

// MarkStepDone 记录用户对"程序验不了的步骤"的手动确认。
func MarkStepDone(stepID uint) error {
	now := timex.Now()
	return database.DB.Model(&model.Step{}).Where("id = ?", stepID).
		Updates(map[string]any{"status": model.StepDone, "done_at": now, "last_error": ""}).Error
}

// MarkStepStarted 记录用户已按指令操作完毕, 等待计时从此刻起算。
func MarkStepStarted(stepID uint) error {
	now := timex.Now()
	return database.DB.Model(&model.Step{}).Where("id = ?", stepID).
		Updates(map[string]any{"status": model.StepWaiting, "started_at": now}).Error
}

// MarkStepSkipped 记录用户决定不做这一步。
// 主要给拆除流程用: 想留着 DNSPod 域名、回退源还有别的主机名在用, 都是合理的"不做"。
func MarkStepSkipped(stepID uint) error {
	return database.DB.Model(&model.Step{}).Where("id = ?", stepID).
		Updates(map[string]any{"status": model.StepSkipped, "last_error": ""}).Error
}

// MarkStepReset 把步骤退回未开始, 下一次巡检重新判定它。
// 用来撤销误点的跳过 —— 跳过本身什么也没做, 不该是个单向门。
func MarkStepReset(stepID uint) error {
	return database.DB.Model(&model.Step{}).Where("id = ?", stepID).
		Updates(map[string]any{
			"status": model.StepPending, "last_error": "", "started_at": nil, "done_at": nil,
		}).Error
}

