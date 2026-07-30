package service

import (
	"sort"

	"github.com/woodchen-ink/splitdns/server/database"
	"github.com/woodchen-ink/splitdns/server/model"
)

// 列表页要显示"这个域名配到哪一步了"。数据一律取本地库里的流程与步骤:
// 逐个域名去三个平台巡检既慢又吃配额, 列表页不背这个成本 —— 实际状态仍然是进详情页按需巡检。

// PlanProgress 是一条流程的完成度概览。
type PlanProgress struct {
	PlanID uint `json:"planId"`
	// Kind setup / teardown; 开放式取值, 前端按映射取文案并保留兜底
	Kind   string `json:"kind"`
	Status string `json:"status"`
	Total  int    `json:"total"`
	// Done 验证通过的步数; Skipped 是用户明确决定不做的步数。两者都算走完
	Done    int `json:"done"`
	Skipped int `json:"skipped"`
	// Current 当前停在哪一步的标题; 全部走完时为空
	Current string `json:"current"`
}

// planProgressByHostname 按域名聚合流程完成度。
//
// 两条 IN 查询搞定, 不逐个域名回查 —— 列表一页 50 个域名就是 100 次往返。
// 同一类型有多条流程时只留一条: 优先还在跑的那条, 否则取最新建的,
// 免得卡片上并排两个"配置"让人不知道该看哪个。
func planProgressByHostname(hostnameIDs []uint) (map[uint][]PlanProgress, error) {
	out := map[uint][]PlanProgress{}
	if len(hostnameIDs) == 0 {
		return out, nil
	}

	var plans []model.Plan
	if err := database.DB.Where("hostname_id IN ?", hostnameIDs).Order("id").Find(&plans).Error; err != nil {
		return nil, err
	}
	if len(plans) == 0 {
		return out, nil
	}

	planIDs := make([]uint, 0, len(plans))
	for _, p := range plans {
		planIDs = append(planIDs, p.ID)
	}
	var steps []model.Step
	if err := database.DB.Where("plan_id IN ?", planIDs).Order("plan_id, seq").Find(&steps).Error; err != nil {
		return nil, err
	}
	stepsOf := map[uint][]model.Step{}
	for _, s := range steps {
		stepsOf[s.PlanID] = append(stepsOf[s.PlanID], s)
	}

	// hostname → kind → 该类型留下的那条; plans 按 id 升序, 后来的覆盖先前的
	kept := map[uint]map[string]PlanProgress{}
	for _, p := range plans {
		byKind := kept[p.HostnameID]
		if byKind == nil {
			byKind = map[string]PlanProgress{}
			kept[p.HostnameID] = byKind
		}
		kind := p.PlanKind()
		if old, ok := byKind[kind]; ok && old.Status == model.PlanRunning && p.Status != model.PlanRunning {
			continue
		}
		byKind[kind] = summarizePlan(p, stepsOf[p.ID])
	}

	for hostnameID, byKind := range kept {
		list := make([]PlanProgress, 0, len(byKind))
		for _, pg := range byKind {
			list = append(list, pg)
		}
		// 按流程 id 排, 不按类型硬编码顺序: 新增流程类型不用回来改这里
		sort.Slice(list, func(i, j int) bool { return list[i].PlanID < list[j].PlanID })
		out[hostnameID] = list
	}
	return out, nil
}

// summarizePlan 把一条流程的步骤压成完成度。
// skipped 与 done 一样算走完 —— 它是终态, 不再验证也不阻塞流程收尾。
func summarizePlan(p model.Plan, steps []model.Step) PlanProgress {
	pg := PlanProgress{PlanID: p.ID, Kind: p.PlanKind(), Status: p.Status, Total: len(steps)}
	for _, s := range steps {
		switch s.Status {
		case model.StepDone:
			pg.Done++
		case model.StepSkipped:
			pg.Skipped++
		default:
			// 步骤按 seq 排好, 第一个没走完的就是当前停在哪
			if pg.Current == "" {
				pg.Current = s.Title
			}
		}
	}
	return pg
}
