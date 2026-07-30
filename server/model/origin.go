package model

import "time"

// 回源类型。取值会随接入的平台增长, 消费侧按"模式识别 + 默认兜底"处理:
// 带 SNI 的一律按自定义源校验, 值形如 IP 的按 A 记录校验, 其余按 CNAME 校验,
// 未识别的类型不静默跳过, 而是原样展示并标注为未知类型。
const (
	// OriginSaaSFallback CF for SaaS 的回退源, 值为 SaaS 区内的橙云主机名
	OriginSaaSFallback = "saas_fallback"
	// OriginSaaSCustom 自定义源服务器, 需要额外的 SNI, 且源站必须能路由该 SNI
	OriginSaaSCustom = "saas_custom"
	// OriginCNAME 任意第三方 CDN / 服务的 CNAME 落点, 如 EdgeOne
	OriginCNAME = "cname"
	// OriginIP 直连源站 IP
	OriginIP = "ip"
)

// Origin 是一个可被多个访问域名复用的回源目标。
// 一个回源改地址, 所有引用它的线路一起变, 不需要逐个域名改。
type Origin struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// Name 备注名, 如 "海外源站" / "EdgeOne 国内"
	Name string `gorm:"column:name;size:128;not null" json:"name"`
	// Kind 回源类型, 见上方常量
	Kind string `gorm:"column:kind;size:32;index;not null" json:"kind"`
	// Value 落点值: 主机名或 IP
	Value string `gorm:"column:value;size:256;not null" json:"value"`
	// Address 落点主机名背后的源站 IP, 程序靠它代建那条橙云记录 (记录已经存在时留空即可)。
	// 双栈落点用逗号 / 空白分隔填多个: IPv4 建 A、IPv6 建 AAAA, 各一条
	Address string `gorm:"column:address;size:128" json:"address"`
	// SNI 回源 TLS 握手使用的 SNI, 仅自定义源服务器需要;
	// 填了就意味着源站上必须存在能路由该名字的 router/vhost, 否则回源 403
	SNI string `gorm:"column:sni;size:256" json:"sni"`
	// Note 自由备注
	Note string `gorm:"column:note;size:512" json:"note"`
}

func (Origin) TableName() string { return "origin" }

// NeedsSNIRoute 判定该回源是否要求源站额外配置 SNI 路由。
// 判据是"有没有 SNI", 不是硬编码 Kind 列表, 新增回源类型时无需改这里。
// SNI 与落点值相同也照样要求 —— 回源 TLS 握手用的就是这个名字, 源站认不出来一样 403。
func (o Origin) NeedsSNIRoute() bool {
	return o.SNI != ""
}
