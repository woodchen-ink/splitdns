package model

import "time"

// Hostname 是一个对外提供服务的访问域名, 它的权威 DNS 已从 CF 父区委派到 DNSPod。
// 一个父区下可以挂任意多个访问域名, 彼此独立。
type Hostname struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// Hostname 对外主机名, 如 i.czl.net
	Hostname string `gorm:"column:hostname;size:253;uniqueIndex;not null" json:"hostname"`
	// ParentZone 主域名所在的 CF zone, 委派 NS 加在这里
	ParentZone string `gorm:"column:parent_zone;size:253;index;not null" json:"parentZone"`
	// SaaSZone 承载自定义主机名的 CF zone; 为空表示该域名不走 CF for SaaS
	SaaSZone string `gorm:"column:saas_zone;size:253;index" json:"saasZone"`
	// DNSPodDomain DNSPod 上托管的域名, 留空取 Hostname 本身
	DNSPodDomain string `gorm:"column:dnspod_domain;size:253" json:"dnspodDomain"`

	// CFCredentialID 访问 CF 用的凭据; 父区与 SaaS 区分属不同账号时用 SaaSCredentialID 覆盖
	CFCredentialID uint `gorm:"column:cf_credential_id;index" json:"cfCredentialId"`
	// SaaSCredentialID SaaS 区所在账号的凭据, 为 0 时复用 CFCredentialID
	SaaSCredentialID uint `gorm:"column:saas_credential_id;index" json:"saasCredentialId"`
	// DNSPodCredentialID 访问 DNSPod 用的凭据
	DNSPodCredentialID uint `gorm:"column:dnspod_credential_id;index" json:"dnspodCredentialId"`

	// Enabled 关闭后不参与巡检, 用于临时下线的域名
	Enabled bool `gorm:"column:enabled;not null;default:true" json:"enabled"`
	Note    string `gorm:"column:note;size:512" json:"note"`

	// Routes 该域名的全部线路落点
	Routes []Route `gorm:"foreignKey:HostnameID;constraint:OnDelete:CASCADE" json:"routes"`
}

func (Hostname) TableName() string { return "hostname" }

// DNSPodZone 返回 DNSPod 上实际托管的域名, 未显式配置时回落到 Hostname。
func (h Hostname) DNSPodZone() string {
	if h.DNSPodDomain != "" {
		return h.DNSPodDomain
	}
	return h.Hostname
}

// SaaSCredential 返回该域名访问 SaaS 区应使用的凭据 ID, 未单独指定时复用父区凭据。
func (h Hostname) SaaSCredential() uint {
	if h.SaaSCredentialID != 0 {
		return h.SaaSCredentialID
	}
	return h.CFCredentialID
}

// Route 是"某条线路应该解析到哪个回源"的绑定关系。
// 同一个 Origin 可以被任意多个 Route 引用, 这是多访问域名共用回源的实现方式。
type Route struct {
	ID         uint `gorm:"primaryKey" json:"id"`
	HostnameID uint `gorm:"column:hostname_id;index;not null" json:"hostnameId"`
	// Line DNSPod 线路名, 如 默认 / 境内 / 境外; 免费版只有这三条
	Line string `gorm:"column:line;size:64;not null" json:"line"`
	// OriginID 该线路指向的回源
	OriginID uint `gorm:"column:origin_id;index;not null" json:"originId"`

	// Origin 预加载的回源详情, 供前端直接渲染, 不需要再调一次接口
	Origin *Origin `gorm:"foreignKey:OriginID" json:"origin"`
}

func (Route) TableName() string { return "route" }
