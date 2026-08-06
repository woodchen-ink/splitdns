package service

import (
	"strings"
	"testing"

	"github.com/woodchen-ink/splitdns/server/model"
)

// hasFinding 判断结果里有没有指定 code 的检查项。
func hasFinding(findings []model.Finding, code string) bool {
	for _, f := range findings {
		if f.Code == code {
			return true
		}
	}
	return false
}

// 回退源值对账: 只看状态发现不了"改了落点没重跑设置"的漂移, 这里守住三条判定边界 ——
// 声明了回退源落点才对账、值等价不误报、没声明的域名不对区共享的回退源下结论。
func TestFallbackOriginMismatch(t *testing.T) {
	declared := model.Hostname{
		Hostname: "i.example.com",
		SaaSZone: "saas.example",
		Routes: []model.Route{
			{Line: "默认", Kind: model.OriginSaaSFallback, Value: "new.saas.example"},
		},
	}
	snap := model.Snapshot{FallbackOrigin: "old.saas.example", FallbackOriginStatus: "active"}
	if !hasFinding(checkSaaS(declared, snap), "saas.fallback_mismatch") {
		t.Error("声明的回退源与实际不符, 应报 saas.fallback_mismatch")
	}

	// 大小写与尾点差异是等价值
	snap.FallbackOrigin = "New.Saas.Example."
	if hasFinding(checkSaaS(declared, snap), "saas.fallback_mismatch") {
		t.Error("值等价时不该报 mismatch")
	}

	// 本域名只用自定义源服务器时, 区共享的回退源归别的域名管, 不对它下结论
	customOnly := model.Hostname{
		Hostname: "i.example.com",
		SaaSZone: "saas.example",
		Routes: []model.Route{
			{Line: "默认", Kind: model.OriginSaaSCustom, Value: "new.saas.example", SNI: "new.saas.example"},
		},
	}
	snap.FallbackOrigin = "old.saas.example"
	if hasFinding(checkSaaS(customOnly, snap), "saas.fallback_mismatch") {
		t.Error("没声明回退源落点的域名不该报 mismatch")
	}

	// 验证层同一口径: 漂移时步骤不能算通过, 会被退回等待
	ok, known, reason := stepSatisfied("saas.fallback_origin", declared, snap)
	if ok || !known {
		t.Errorf("回退源漂移时步骤不该算通过, ok=%v known=%v", ok, known)
	}
	if !strings.Contains(reason, "new.saas.example") {
		t.Errorf("退回理由应说明期望值, 实际 %q", reason)
	}
}

// 自定义主机名那一步的验证不能只看"主机名存在": 源服务器字段配在它身上,
// 漂移时步骤必须退回等待 —— 一直显示完成的话前端不渲染执行按钮, 巡检报的错没有入口能修。
func TestCustomHostnameStepRetreatsOnOriginDrift(t *testing.T) {
	h := model.Hostname{
		Hostname: "i.example.com",
		SaaSZone: "saas.example",
		Routes: []model.Route{
			{Line: "默认", Kind: model.OriginSaaSCustom, Value: "new.saas.example", SNI: "new.saas.example"},
		},
	}
	snap := model.Snapshot{CustomHostname: model.CustomHostnameState{Exists: true, CustomOrigin: ""}}

	ok, known, reason := stepSatisfied("saas.custom_hostname", h, snap)
	if ok || !known {
		t.Errorf("源服务器与配置不符时步骤不该算通过, ok=%v known=%v", ok, known)
	}
	if !strings.Contains(reason, "new.saas.example") {
		t.Errorf("退回理由应说明期望的源服务器, 实际 %q", reason)
	}

	snap.CustomHostname.CustomOrigin = "New.Saas.Example."
	if ok, _, reason := stepSatisfied("saas.custom_hostname", h, snap); !ok {
		t.Errorf("源服务器等价时步骤应算通过, 退回理由 %q", reason)
	}
}
