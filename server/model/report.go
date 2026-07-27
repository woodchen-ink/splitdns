package model

// Level 是单条检查结论的严重程度。
type Level string

const (
	LevelOK    Level = "ok"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// Rank 返回严重程度的可比较权重, 用于聚合出一个域名 / 一批域名的最高级别。
func (l Level) Rank() int {
	switch l {
	case LevelError:
		return 2
	case LevelWarn:
		return 1
	default:
		return 0
	}
}

// Finding 是一条检查结论。Code 是稳定标识, 前端按前缀分组 + 通用渲染,
// 后端新增检查项不需要前端同步发版。
type Finding struct {
	Level Level `json:"level"`
	// Code 检查项标识, 形如 delegation.missing / dcv.incomplete / origin.sni_route
	Code string `json:"code"`
	// Title 一句话结论
	Title string `json:"title"`
	// Detail 实际观测到的值, 供人工核对
	Detail string `json:"detail"`
	// Fix 可执行的修复建议, 无则为空字符串
	Fix string `json:"fix"`
}

// Snapshot 是一个访问域名在三个平台上的实际状态, 由后端聚合完毕, 前端直接渲染。
type Snapshot struct {
	// Delegation CF 父区里该子域名的 NS 委派记录值
	Delegation []string `json:"delegation"`
	// DNSPodNameservers DNSPod 实际分配给该域名的 NS
	DNSPodNameservers []string `json:"dnspodNameservers"`
	// DNSPodEnabled DNSPod 上该域名的解析是否在对外生效
	DNSPodEnabled bool `json:"dnspodEnabled"`
	// DNSPodStatus DNSPod 返回的原始域名状态, 判定存疑时用它对照
	DNSPodStatus string `json:"dnspodStatus"`
	// ShadowedRecords CF 父区里被委派遮蔽的记录 (名字 + 类型)
	ShadowedRecords []string `json:"shadowedRecords"`

	// FallbackOrigin SaaS 区当前的回退源主机名
	FallbackOrigin string `json:"fallbackOrigin"`
	// FallbackOriginStatus 回退源状态, 非 active 时自定义主机名无法完成验证
	FallbackOriginStatus string `json:"fallbackOriginStatus"`

	CustomHostname CustomHostnameState `json:"customHostname"`

	// Records DNSPod 上该域名的全部解析记录
	Records []DNSRecord `json:"records"`
}

// CustomHostnameState 是 Cloudflare for SaaS 自定义主机名的关键状态。
type CustomHostnameState struct {
	Exists bool `json:"exists"`
	// Status 主机名验证状态 (active / pending / blocked ...)
	Status string `json:"status"`
	// SSLStatus 证书状态
	SSLStatus string `json:"sslStatus"`
	// ValidationMethod txt / http / email
	ValidationMethod string `json:"validationMethod"`
	// CertExpiresAt RFC3339, 无证书时为空
	CertExpiresAt string `json:"certExpiresAt"`
	// CertAuthority 签发 CA, CF 会在不同 CA 之间切换
	CertAuthority string `json:"certAuthority"`
	// CustomOrigin 自定义源服务器, 为空表示走默认回退源
	CustomOrigin string `json:"customOrigin"`
	// CustomOriginSNI 回源 TLS 握手使用的 SNI
	CustomOriginSNI string `json:"customOriginSni"`
	// OwnershipTXT CF 要求的归属验证 TXT
	OwnershipTXT TXTRequirement `json:"ownershipTxt"`
	// DCVTXT CF 要求的证书 DCV TXT; 证书带通配符 SAN 时会有多条同名不同值的记录
	DCVTXT []TXTRequirement `json:"dcvTxt"`
	// MinTLSVersion 最低 TLS 版本
	MinTLSVersion string `json:"minTlsVersion"`
}

// TXTRequirement 是一条需要落到权威 DNS 上的 TXT 记录。
type TXTRequirement struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// DNSRecord 是 DNSPod 上的一条解析记录。
type DNSRecord struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Line    string `json:"line"`
	Value   string `json:"value"`
	TTL     uint64 `json:"ttl"`
	Enabled bool   `json:"enabled"`
}

// HostnameReport 是单个访问域名的完整巡检结果。
type HostnameReport struct {
	HostnameID uint   `json:"hostnameId"`
	Hostname   string `json:"hostname"`

	Snapshot Snapshot  `json:"snapshot"`
	Findings []Finding `json:"findings"`
	// Level 该域名的最高严重级别, 列表页直接按它着色
	Level Level `json:"level"`
	// Summary 一句话总结
	Summary string `json:"summary"`
	// CheckedAt RFC3339
	CheckedAt string `json:"checkedAt"`
}
