package service

import (
	"fmt"

	"github.com/woodchen-ink/splitdns/server/database"
	"github.com/woodchen-ink/splitdns/server/model"
	"gorm.io/gorm"
)

// CreatePlan 为某个访问域名取出 (或首次生成) 一条流程。kind 决定是配置还是拆除, 两者互不干扰:
// 同一个域名可以同时挂着一条配置流程和一条拆除流程, 各按各的步骤走。
//
// 同类型只留一条, 已有的一律复用并把步骤对齐到当前配置, 不重建。
func CreatePlan(hostnameID uint, kind string) (*model.Plan, error) {
	h, err := HostnameByID(hostnameID)
	if err != nil {
		return nil, err
	}

	var want []model.Step
	switch kind {
	case model.PlanSetup:
		want = buildSteps(*h)
	case model.PlanTeardown:
		want = buildTeardownSteps(*h)
	default:
		return nil, fmt.Errorf("未知的流程类型 %q", kind)
	}

	existing, err := latestPlan(hostnameID, kind)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if err := syncSteps(existing, want); err != nil {
			return nil, err
		}
		return existing, nil
	}

	plan := model.Plan{HostnameID: hostnameID, Kind: kind, Status: model.PlanRunning, Steps: want}
	if err := database.DB.Create(&plan).Error; err != nil {
		return nil, fmt.Errorf("创建流程失败: %w", err)
	}
	return &plan, nil
}

// latestPlan 取该域名这一类型下最近的一条流程, 没有则返回 nil。
//
// 不限定"还在跑的": 走完最后一步流程就变成 done, 再按 running 找必然落空, 于是又建一条全新的,
// 用户手动确认过的步骤全部回到未开始。能自动验证的步骤会被下一轮巡检立刻翻回完成,
// 所以看起来就是"只有程序验不了的那一步反复退回去"。
func latestPlan(hostnameID uint, kind string) (*model.Plan, error) {
	q := database.DB.Preload("Steps").Where("hostname_id = ?", hostnameID)
	if kind == model.PlanSetup {
		// 加上 kind 这一列之前建的流程都是配置流程, 值是空串
		q = q.Where("kind IN ?", []string{model.PlanSetup, ""})
	} else {
		q = q.Where("kind = ?", kind)
	}
	// 用 Find 而不是 First: "还没有流程"是最常见的正常路径,
	// First 会把它当成 ErrRecordNotFound 记一条错误日志, 纯噪音
	var found []model.Plan
	if err := q.Order("id DESC").Limit(1).Find(&found).Error; err != nil {
		return nil, fmt.Errorf("查询已有流程失败: %w", err)
	}
	if len(found) == 0 {
		return nil, nil
	}
	return &found[0], nil
}

// syncSteps 把已有流程的步骤对齐到当前配置。
//
// 步骤清单是域名配置的投影: 配上 SaaS 区就该多出那几步, 撤掉自定义源就该少一步。
// 复用流程如果不对齐, 改完配置的域名会一直挂着一份过时的步骤单。
// 对齐只覆盖模板信息 (顺序 / 标题 / 指令 / 执行方式 / 能否自动验证), 状态与时间戳一律保留 ——
// 那些是用户已经做过的事, 不能因为进一次页面就清零。
func syncSteps(plan *model.Plan, want []model.Step) error {
	have := make(map[string]*model.Step, len(plan.Steps))
	for i := range plan.Steps {
		have[plan.Steps[i].Key] = &plan.Steps[i]
	}
	wanted := make(map[string]bool, len(want))
	merged := make([]model.Step, 0, len(want))

	err := database.DB.Transaction(func(tx *gorm.DB) error {
		for _, w := range want {
			wanted[w.Key] = true
			old, ok := have[w.Key]
			if !ok {
				w.PlanID = plan.ID
				if err := tx.Create(&w).Error; err != nil {
					return err
				}
				merged = append(merged, w)
				continue
			}
			old.Seq, old.Title, old.Instruction = w.Seq, w.Title, w.Instruction
			old.Mode, old.ETASeconds, old.Verifiable = w.Mode, w.ETASeconds, w.Verifiable
			if err := tx.Save(old).Error; err != nil {
				return err
			}
			merged = append(merged, *old)
		}
		// 不再适用的步骤直接删: 留着它做过与否都没有意义, 还会一直卡着流程收尾
		for _, s := range plan.Steps {
			if wanted[s.Key] {
				continue
			}
			if err := tx.Delete(&model.Step{}, s.ID).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("对齐流程步骤失败: %w", err)
	}
	plan.Steps = merged
	return nil
}

// buildSteps 按域名的实际配置生成步骤序列。
// 不适用的步骤直接不生成 (比如没配 SaaS 区就没有回退源和证书相关步骤),
// 而不是生成后标记跳过, 免得界面上一堆灰条干扰。
func buildSteps(h model.Hostname) []model.Step {
	var steps []model.Step
	seq := 0
	add := func(s model.Step) {
		seq++
		s.Seq = seq
		s.Status = model.StepPending
		steps = append(steps, s)
	}

	useSaaS := h.SaaSZone != ""

	if useSaaS {
		add(model.Step{
			Key:   "saas.fallback_origin",
			Title: "在 SaaS 区设置回退源",
			Mode:  model.StepManual,
			Instruction: fmt.Sprintf(
				"到 CF 的 %s 区 → SSL/TLS → 自定义主机名, 先启用 Cloudflare for SaaS。\n"+
					"在该区建一条橙云(Proxied)记录指向你的源站, 再把这个主机名填进「回退源」。\n"+
					"回退源状态不是「有效」时, 后面的自定义主机名验证不会通过。",
				h.SaaSZone),
			ETASeconds: 60,
			Verifiable: true,
		})
		add(model.Step{
			Key:   "saas.custom_hostname",
			Title: "添加自定义主机名",
			Mode:  model.StepManual,
			Instruction: fmt.Sprintf(
				"在同一页面点「添加自定义主机名」, 填 %s。\n"+
					"证书验证方式必须选 TXT —— HTTP 验证要求域名已经指向 CF 才能过, "+
					"而这时解析还没切过来, 会卡死。",
				h.Hostname),
			ETASeconds: 30,
			Verifiable: true,
		})
	}

	if h.Delegated() {
		add(model.Step{
			Key:   "dnspod.zone",
			Title: "在 DNSPod 添加域名",
			Mode:  model.StepManual,
			Instruction: fmt.Sprintf(
				"到 DNSPod「添加域名」处直接填 %s (子域名可以当独立域名添加, 免费版即可)。\n"+
					"添加后进域名详情, 记下它分配给你的那组 NS —— 后面委派要用, 以控制台显示的为准。",
				h.DNSPodZone()),
			ETASeconds: 30,
			Verifiable: true,
		})
	} else {
		// 直托模式的前提就是域名已经在 DNSPod 上, 这一步通常一进来就自动通过;
		// 留着它是为了兜住"域名被暂停 / 被移出账号"这类前提被破坏的情况
		add(model.Step{
			Key:   "dnspod.zone",
			Title: "确认域名在 DNSPod 上且解析已启用",
			Mode:  model.StepManual,
			Instruction: fmt.Sprintf(
				"确认 %s 已经加进这个腾讯云账号的 DNSPod 并处于启用状态。\n"+
					"这个域名的解析本来就在 DNSPod 上, 不需要任何委派操作。",
				h.DNSPodZone()),
			ETASeconds: 30,
			Verifiable: true,
		})
	}

	if useSaaS {
		add(model.Step{
			Key:         "dnspod.dcv_txt",
			Title:       "写入 CF 要求的验证 TXT",
			Mode:        model.StepManual,
			Instruction: "等这一步刷新出具体记录值后再操作。CF 的 DCV 记录在证书带通配符 SAN 时会有多条同名不同值, 必须全部写进去, 少一条证书就签不出来。",
			ETASeconds:  300,
			Verifiable:  true,
		})
	}

	add(model.Step{
		Key:         "dnspod.routes",
		Title:       "配置分线路解析记录",
		Mode:        model.StepManual,
		Instruction: "等这一步刷新出具体记录值后再操作。TTL 用 600 秒 —— DNSPod 免费版最低就是它, 填更小会被拒。",
		ETASeconds:  300,
		Verifiable:  true,
	})

	if h.Delegated() {
		add(model.Step{
			Key:   "cf.cleanup",
			Title: "清空父区里该子域名的旧记录",
			Mode:  model.StepManual,
			Instruction: fmt.Sprintf(
				"到 CF 的 %s 区, 删掉 %s 以及它下面所有已有记录。\n"+
					"留着不会报错, 但委派之后它们会变成 shadowed records —— 列表里看得见, 实际一条都不生效。",
				h.ParentZone, h.Hostname),
			ETASeconds: 30,
			Verifiable: true,
		})

		add(model.Step{
			Key:   "cf.delegation",
			Title: "在父区加 NS 委派",
			Mode:  model.StepManual,
			Instruction: fmt.Sprintf(
				"在 CF 的 %s 区加 NS 记录, 名称填 %s, 内容填 DNSPod 分配的那组 NS (通常两条)。\n"+
					"代理状态显示「仅 DNS」是正常的, NS 类型没有橙云开关。\n"+
					"这一步只影响这一个名字, 父区其它记录和注册商那边都不用动。",
				h.ParentZone, relativeName(h.Hostname, h.ParentZone)),
			ETASeconds: 600,
			Verifiable: true,
		})
	}

	if useSaaS {
		add(model.Step{
			Key:         "saas.cert",
			Title:       "等待证书签发并部署",
			Mode:        model.StepWait,
			Instruction: "不需要操作。CF 会在验证记录生效后自动签发并部署证书, 主机名状态和证书状态都变成「有效」即完成。",
			ETASeconds:  600,
			Verifiable:  true,
		})
	}

	if needsSNIStep(h) {
		add(model.Step{
			Key:   "origin.sni_route",
			Title: "在源站上补 SNI 对应的路由",
			Mode:  model.StepManual,
			Instruction: "这个域名用了自定义源服务器。CF 回源时 Host 头是访问域名, 但 TLS 握手的 SNI 是你填的那个源服务器名字, " +
				"源站上的 Traefik / Nginx 匹配不到对应 router 就会直接返回 403。\n" +
				"在源站上给这个 SNI 名字随便挂一个 router / vhost 即可, 指向哪个服务、有没有证书都无所谓。\n" +
				"这一步程序无法自动验证, 做完请手动确认。",
			ETASeconds: 60,
			Verifiable: false,
		})
	}

	return steps
}

// needsSNIStep 判定该域名是否存在需要源站额外配置 SNI 路由的回源。
func needsSNIStep(h model.Hostname) bool {
	for _, r := range h.Routes {
		if t := r.Target(); t != nil && t.NeedsSNIRoute() {
			return true
		}
	}
	return false
}
