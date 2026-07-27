package cloudflare

import (
	"context"
	"net/url"
)

// FallbackOrigin 是 SaaS 区的回退源状态。回退源不是 active 时,
// 该区下所有自定义主机名都无法完成验证。
type FallbackOrigin struct {
	Origin string `json:"origin"`
	Status string `json:"status"`
}

// GetFallbackOrigin 读取 SaaS 区当前的回退源。
// 没设置过回退源是正常状态 (刚启用 SaaS, 或者已经拆掉), 返回零值而不是错误。
func (c *Client) GetFallbackOrigin(ctx context.Context, zoneID string) (FallbackOrigin, error) {
	var fo FallbackOrigin
	err := c.get(ctx, "/zones/"+zoneID+"/custom_hostnames/fallback_origin", nil, &fo)
	if IsNotFound(err) {
		return FallbackOrigin{}, nil
	}
	return fo, err
}

// CustomHostname 是自定义主机名接口返回的字段子集。
type CustomHostname struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
	Status   string `json:"status"`
	SSL      struct {
		Status               string `json:"status"`
		Method               string `json:"method"`
		CertificateAuthority string `json:"certificate_authority"`
		// ValidationRecords 是 CF 要求写到权威 DNS 的 DCV 记录。
		// 证书带通配符 SAN 时这里会有多条 txt_name 相同、txt_value 不同的记录, 必须全部落地。
		ValidationRecords []struct {
			TxtName  string `json:"txt_name"`
			TxtValue string `json:"txt_value"`
			HTTPURL  string `json:"http_url"`
		} `json:"validation_records"`
		ValidationErrors []struct {
			Message string `json:"message"`
		} `json:"validation_errors"`
		Certificates []struct {
			ExpiresOn string `json:"expires_on"`
			Issuer    string `json:"issuer"`
		} `json:"certificates"`
		Settings struct {
			MinTLSVersion string `json:"min_tls_version"`
		} `json:"settings"`
	} `json:"ssl"`
	OwnershipVerification struct {
		Type  string `json:"type"`
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"ownership_verification"`
	CustomOriginServer string `json:"custom_origin_server"`
	CustomOriginSNI    string `json:"custom_origin_sni"`
}

// ListCustomHostnames 列出 SaaS 区里的自定义主机名, 只取一页。
// 用途只是判断"除了自己还有没有别人在用这个区的回退源", 不需要精确总数, 因此不翻页。
func (c *Client) ListCustomHostnames(ctx context.Context, zoneID string) ([]CustomHostname, error) {
	q := url.Values{"per_page": {"50"}}
	var list []CustomHostname
	err := c.get(ctx, "/zones/"+zoneID+"/custom_hostnames", q, &list)
	return list, err
}

// FindCustomHostname 在 SaaS 区里按精确主机名查自定义主机名。
// 不存在时返回 nil, nil —— "没配"是正常状态之一, 由调用方判定是否算问题。
func (c *Client) FindCustomHostname(ctx context.Context, zoneID, hostname string) (*CustomHostname, error) {
	q := url.Values{"hostname": {hostname}, "per_page": {"50"}}
	var list []CustomHostname
	if err := c.get(ctx, "/zones/"+zoneID+"/custom_hostnames", q, &list); err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].Hostname == hostname {
			return &list[i], nil
		}
	}
	return nil, nil
}
