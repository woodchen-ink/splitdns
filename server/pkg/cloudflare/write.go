package cloudflare

import (
	"context"
	"fmt"
	"net/http"
	"strings"
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
// 记录已经不在了当成功: 删到一半失败重来时, 前面删过的不该把整批卡住。
func (c *Client) DeleteRecord(ctx context.Context, zoneID, recordID string) error {
	err := c.do(ctx, http.MethodDelete, "/zones/"+zoneID+"/dns_records/"+recordID, nil, nil, nil)
	if IsNotFound(err) {
		return nil
	}
	return err
}

// DeleteCustomHostname 删除自定义主机名, CF 会一并吊销它的证书。
func (c *Client) DeleteCustomHostname(ctx context.Context, zoneID, hostnameID string) error {
	err := c.do(ctx, http.MethodDelete, "/zones/"+zoneID+"/custom_hostnames/"+hostnameID, nil, nil, nil)
	if IsNotFound(err) {
		return nil
	}
	return err
}

// DeleteFallbackOrigin 清掉 SaaS 区的回退源设置。
// 这是整个区共享的配置, 调用方必须先确认区里已经没有别的自定义主机名在用它。
func (c *Client) DeleteFallbackOrigin(ctx context.Context, zoneID string) error {
	err := c.do(ctx, http.MethodDelete, "/zones/"+zoneID+"/custom_hostnames/fallback_origin", nil, nil, nil)
	if IsNotFound(err) {
		return nil
	}
	return err
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

// UpdateCustomOrigin 改自定义主机名的源服务器与 SNI。origin 传空表示回到默认回退源。
//
// sni 只在与源服务器不同名时才发: CF 默认就拿源服务器主机名当回源 SNI, 而显式设置这个字段
// 是企业版 SSL for SaaS 才有的能力, 非企业账号发了会被 1456 拒掉。
func (c *Client) UpdateCustomOrigin(ctx context.Context, zoneID, hostnameID, origin, sni string) error {
	payload := map[string]any{"custom_origin_server": origin}
	if sni != "" && !strings.EqualFold(sni, origin) {
		payload["custom_origin_sni"] = sni
	}
	err := c.do(ctx, http.MethodPatch, "/zones/"+zoneID+"/custom_hostnames/"+hostnameID, nil, payload, nil)
	return explainSNIRestriction(err)
}

// explainSNIRestriction 把 CF 的 1456 翻译成能直接照做的说明。
func explainSNIRestriction(err error) error {
	if err == nil || !strings.Contains(err.Error(), "1456") {
		return err
	}
	return fmt.Errorf("单独指定回源 SNI 是企业版 SSL for SaaS 才有的功能, 当前账号用不了。" +
		"把这个回源的 SNI 留空或改成与源服务器同名即可 —— CF 默认就拿源服务器主机名当 SNI")
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
	// 与源服务器同名的 SNI 不发, 理由见 UpdateCustomOrigin
	if sni != "" && !strings.EqualFold(sni, customOrigin) {
		in.CustomOriginSNI = sni
	}

	var out CustomHostname
	if err := c.do(ctx, http.MethodPost, "/zones/"+zoneID+"/custom_hostnames", nil, in, &out); err != nil {
		return nil, explainSNIRestriction(err)
	}
	return &out, nil
}
