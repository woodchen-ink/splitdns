package service

import (
	"fmt"

	"github.com/woodchen-ink/splitdns/server/database"
	"github.com/woodchen-ink/splitdns/server/model"
)

// CreatePlan 为某个访问域名生成一条配置流程。
// 已有未完成流程时直接复用, 避免同一个域名并行两套步骤互相打架。
func CreatePlan(hostnameID uint) (*model.Plan, error) {
	h, err := HostnameByID(hostnameID)
	if err != nil {
		return nil, err
	}

	var existing model.Plan
	err = database.DB.Preload("Steps").
		Where("hostname_id = ? AND status = ?", hostnameID, "running").
		First(&existing).Error
	if err == nil {
		return &existing, nil
	}

	plan := model.Plan{HostnameID: hostnameID, Status: "running", Steps: buildSteps(*h)}
	if err := database.DB.Create(&plan).Error; err != nil {
		return nil, fmt.Errorf("创建流程失败: %w", err)
	}
	return &plan, nil
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
		Instruction: "等这一步刷新出具体记录值后再操作。TTL 先调到 60~120 秒, 切换出问题好回滚。",
		ETASeconds:  300,
		Verifiable:  true,
	})

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
		if r.Origin != nil && r.Origin.NeedsSNIRoute() {
			return true
		}
	}
	return false
}
