package cloudflare

import (
	"context"
	"net/http"
)

// NewRecord 是创建 DNS 记录的入参。
type NewRecord struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	// Proxied 仅对 A/AAAA/CNAME 有意义; NS 记录必须为 false
	Proxied bool `json:"proxied"`
	// TTL 1 表示自动
	TTL int `json:"ttl"`
	// Comment 写上来源, 便于以后在 CF 面板里认出是本工具建的
	Comment string `json:"comment,omitempty"`
}

// CreateRecord 在指定 zone 建一条 DNS 记录。
func (c *Client) CreateRecord(ctx context.Context, zoneID string, rec NewRecord) (DNSRecord, error) {
	if rec.TTL == 0 {
		rec.TTL = 1
	}
	var out DNSRecord
	err := c.do(ctx, http.MethodPost, "/zones/"+zoneID+"/dns_records", nil, rec, &out)
	return out, err
}

// DeleteRecord 删除一条 DNS 记录。调用方必须先向用户展示待删列表并取得确认。
func (c *Client) DeleteRecord(ctx context.Context, zoneID, recordID string) error {
	return c.do(ctx, http.MethodDelete, "/zones/"+zoneID+"/dns_records/"+recordID, nil, nil, nil)
}

// SetFallbackOrigin 设置 SaaS 区的回退源。origin 必须是该区内一条已存在的橙云记录。
func (c *Client) SetFallbackOrigin(ctx context.Context, zoneID, origin string) (FallbackOrigin, error) {
	payload := map[string]string{"origin": origin}
	var out FallbackOrigin
	err := c.do(ctx, http.MethodPut, "/zones/"+zoneID+"/custom_hostnames/fallback_origin", nil, payload, &out)
	return out, err
}

// NewCustomHostname 是创建自定义主机名的入参。
type NewCustomHostname struct {
	Hostname string `json:"hostname"`
	SSL      struct {
		Method string `json:"method"`
		Type   string `json:"type"`
		// Settings 里显式给出最低 TLS 版本, 不吃 CF 默认的 1.0
		Settings struct {
			MinTLSVersion string `json:"min_tls_version"`
		} `json:"settings"`
	} `json:"ssl"`
	CustomOriginServer string `json:"custom_origin_server,omitempty"`
	CustomOriginSNI    string `json:"custom_origin_sni,omitempty"`
}

// UpdateCustomOrigin 改自定义主机名的源服务器与 SNI。
// 两个值传空表示回到默认回退源。
func (c *Client) UpdateCustomOrigin(ctx context.Context, zoneID, hostnameID, origin, sni string) error {
	payload := map[string]any{
		"custom_origin_server": origin,
		"custom_origin_sni":    sni,
	}
	return c.do(ctx, http.MethodPatch, "/zones/"+zoneID+"/custom_hostnames/"+hostnameID, nil, payload, nil)
}

// CreateCustomHostname 创建自定义主机名。
// 验证方式固定 TXT: 解析尚未切到 CF 时 HTTP 验证必然失败, 这是配置顺序决定的, 不给调用方选错的机会。
func (c *Client) CreateCustomHostname(ctx context.Context, zoneID, hostname, customOrigin, sni string) (*CustomHostname, error) {
	var in NewCustomHostname
	in.Hostname = hostname
	in.SSL.Method = "txt"
	in.SSL.Type = "dv"
	in.SSL.Settings.MinTLSVersion = "1.2"
	in.CustomOriginServer = customOrigin
	in.CustomOriginSNI = sni

	var out CustomHostname
	if err := c.do(ctx, http.MethodPost, "/zones/"+zoneID+"/custom_hostnames", nil, in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
