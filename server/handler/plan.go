package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/woodchen-ink/go-web-utils/resputil"
	"github.com/woodchen-ink/splitdns/server/service"
)

// CreatePlan POST /api/hostnames/{id}/plan 为某个域名建 (或复用) 配置流程
func CreatePlan(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	plan, err := service.CreatePlan(id)
	if err != nil {
		resputil.Fail(w, 500, err.Error())
		return
	}
	// 建完立刻巡检一次, 已经手动做过的步骤直接显示为完成
	view, err := service.RefreshPlan(r.Context(), plan.ID)
	if err != nil {
		resputil.Fail(w, 500, err.Error())
		return
	}
	resputil.OK(w, view)
}

// GetPlan GET /api/plans/{id} 刷新并返回流程视图
func GetPlan(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	view, err := service.RefreshPlan(r.Context(), id)
	if err != nil {
		resputil.Fail(w, 500, err.Error())
		return
	}
	resputil.OK(w, view)
}

// applyRequest 是执行步骤的入参。
type applyRequest struct {
	StepID uint `json:"stepId"`
	// Confirm 破坏性操作的二次确认
	Confirm bool `json:"confirm"`
}

// ApplyStep POST /api/plans/{id}/apply 让程序执行某一步
func ApplyStep(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req applyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		resputil.Fail(w, 400, "请求体格式错误: "+err.Error())
		return
	}

	msg, err := service.ApplyStep(r.Context(), id, req.StepID, req.Confirm)
	if err != nil {
		// 需要二次确认时用专门的业务码, 前端据此弹确认框并回显待删清单
		if errors.Is(err, service.ErrNeedConfirm) {
			resputil.Fail(w, 4090, err.Error())
			return
		}
		resputil.Fail(w, 500, err.Error())
		return
	}

	view, err := service.RefreshPlan(r.Context(), id)
	if err != nil {
		resputil.Fail(w, 500, err.Error())
		return
	}
	resputil.OKMsg(w, view, msg)
}

// MarkStep POST /api/plans/{id}/mark 记录用户对某步的手动操作
// action=started 表示"我照做了, 开始等生效"; action=done 表示"程序验不了的步骤我确认完成"
func MarkStep(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		StepID uint   `json:"stepId"`
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		resputil.Fail(w, 400, "请求体格式错误: "+err.Error())
		return
	}

	var err error
	switch req.Action {
	case "done":
		err = service.MarkStepDone(req.StepID)
	case "started":
		err = service.MarkStepStarted(req.StepID)
	default:
		resputil.Fail(w, 400, "action 只能是 started 或 done")
		return
	}
	if err != nil {
		resputil.Fail(w, 500, err.Error())
		return
	}

	view, err := service.RefreshPlan(r.Context(), id)
	if err != nil {
		resputil.Fail(w, 500, err.Error())
		return
	}
	resputil.OK(w, view)
}
