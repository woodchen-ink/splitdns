package model

import "time"

// 步骤执行方式。manual 与 auto 的差别只在"谁来动手", 验证逻辑完全一致,
// 因此把某一步从 manual 改成 auto 不需要改验证代码。
const (
	// StepManual 需要用户去对应平台手动操作, 程序只给指令并验证结果
	StepManual = "manual"
	// StepAuto 程序调 API 完成
	StepAuto = "auto"
	// StepWait 无需任何人操作, 纯等待外部系统收敛 (如证书签发)
	StepWait = "wait"
)

// 步骤状态。
const (
	StepPending = "pending" // 还没轮到 / 还没开始做
	StepWaiting = "waiting" // 用户已操作或已触发, 等待生效
	StepDone    = "done"
	StepFailed  = "failed"
	StepSkipped = "skipped" // 当前配置下不适用
)

// 流程类型。两种流程共用同一套步骤机制 (持久化、巡检验证、退回),
// 差别只在步骤清单和每步做什么。
const (
	// PlanSetup 把访问域名配置到位
	PlanSetup = "setup"
	// PlanTeardown 反过来把各平台上的痕迹一处处撤掉
	PlanTeardown = "teardown"
)

// 流程状态。
const (
	PlanRunning = "running"
	PlanDone    = "done"
)

// Plan 是一次"把某个访问域名配置到位"或"把它拆干净"的流程实例。
// 持久化是为了让用户关掉页面后能接着走 —— 这个流程里有多步需要等 DNS 生效, 天然跨会话。
type Plan struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
	HostnameID uint      `gorm:"column:hostname_id;index;not null" json:"hostnameId"`

	// Kind setup / teardown; 早于这个字段的数据里是空串, 一律按 setup 解释
	Kind string `gorm:"column:kind;size:16;not null;default:setup" json:"kind"`
	// Status running / done
	Status string `gorm:"column:status;size:16;not null;default:running" json:"status"`

	Steps []Step `gorm:"foreignKey:PlanID;constraint:OnDelete:CASCADE" json:"steps"`
}

func (Plan) TableName() string { return "plan" }

// PlanKind 返回流程类型, 兼容加上这一列之前建的流程。
func (p Plan) PlanKind() string {
	if p.Kind == "" {
		return PlanSetup
	}
	return p.Kind
}

// Step 是流程中的一步。Key 是稳定标识, 验证逻辑按 Key 分派;
// 前端按 Key 找不到专属渲染时回落通用渲染, 后端新增步骤不需要前端同步发版。
type Step struct {
	ID     uint `gorm:"primaryKey" json:"id"`
	PlanID uint `gorm:"column:plan_id;index;not null" json:"planId"`
	// Seq 展示顺序
	Seq int `gorm:"column:seq;not null" json:"seq"`
	// Key 步骤标识, 如 saas.fallback_origin / dnspod.dcv_txt / cf.delegation
	Key string `gorm:"column:key;size:64;not null" json:"key"`

	Title string `gorm:"column:title;size:128;not null" json:"title"`
	// Instruction 手动步骤要用户照做的具体指令, 精确到在哪个面板填什么值
	Instruction string `gorm:"column:instruction;type:text" json:"instruction"`
	// Mode manual / auto / wait
	Mode   string `gorm:"column:mode;size:16;not null" json:"mode"`
	Status string `gorm:"column:status;size:16;not null;default:pending" json:"status"`

	// ETASeconds 该步操作后预计多久生效, 供前端显示"预计还要等多久"
	ETASeconds int `gorm:"column:eta_seconds" json:"etaSeconds"`
	// StartedAt 用户点"我操作完了"或程序执行完的时刻, 等待计时从这里起算
	StartedAt *time.Time `gorm:"column:started_at" json:"startedAt"`
	// DoneAt 验证通过的时刻
	DoneAt *time.Time `gorm:"column:done_at" json:"doneAt"`
	// LastCheckedAt 最近一次验证时间
	LastCheckedAt *time.Time `gorm:"column:last_checked_at" json:"lastCheckedAt"`
	// LastError 最近一次验证未通过的原因, 通过后清空
	LastError string `gorm:"column:last_error;type:text" json:"lastError"`

	// Verifiable 该步能否由程序自动验证; 为 false 的步骤 (如源站 SNI 路由) 只能用户自行确认。
	// 不能带 default 标签: GORM 对有默认值的字段会跳过零值, false 会被悄悄写成 true,
	// 那一步就会先显示"自动执行", 巡检一轮后又翻成"需人工确认"
	Verifiable bool `gorm:"column:verifiable;not null" json:"verifiable"`
}

func (Step) TableName() string { return "step" }
