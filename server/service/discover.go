package service

import (
	"context"
	"fmt"

	"github.com/woodchen-ink/splitdns/server/model"
)

// 配置里那些要手打的域名, 在平台上本来就存在。这一组接口把它们读出来供界面直接选,
// 少一次手打就少一类"多打一个字母查半天"的问题。

// CloudflareZones 列出某份 CF 凭据可见的 zone。
func CloudflareZones(ctx context.Context, credentialID uint) ([]string, error) {
	cf, err := cloudflareClient(credentialID)
	if err != nil {
		return nil, err
	}
	return cf.ListZoneNames(ctx)
}

// DNSPodDomains 列出某份腾讯云凭据下的域名。
func DNSPodDomains(ctx context.Context, credentialID uint) ([]string, error) {
	dp, err := dnspodClient(credentialID)
	if err != nil {
		return nil, err
	}
	return dp.ListDomainNames(ctx)
}

// SaaSOriginOption 是 SaaS 区里可以拿来当回退源的候选记录。
type SaaSOriginOption struct {
	Hostname string `json:"hostname"`
	Content  string `json:"content"`
	// IsCurrent 是否已经是该区当前的回退源
	IsCurrent bool `json:"isCurrent"`
}

// SaaSOrigins 列出某个 SaaS 区里所有橙云记录, 并标出当前回退源。
// 回退源必须是本区内的橙云记录, 所以灰云的直接过滤掉, 不给选错的机会。
func SaaSOrigins(ctx context.Context, credentialID uint, zoneName string) ([]SaaSOriginOption, error) {
	if zoneName == "" {
		return nil, fmt.Errorf("没有指定 SaaS 区")
	}
	cf, err := cloudflareClient(credentialID)
	if err != nil {
		return nil, err
	}
	zoneID, err := cf.ZoneIDByName(ctx, zoneName)
	if err != nil {
		return nil, err
	}

	current := ""
	if fo, err := cf.GetFallbackOrigin(ctx, zoneID); err == nil {
		current = fo.Origin
	}

	records, err := cf.ListRecords(ctx, zoneID, "", "")
	if err != nil {
		return nil, err
	}
	out := make([]SaaSOriginOption, 0, len(records))
	for _, r := range records {
		if !r.Proxied {
			continue
		}
		out = append(out, SaaSOriginOption{
			Hostname:  r.Name,
			Content:   r.Content,
			IsCurrent: sameName(r.Name, current),
		})
	}
	return out, nil
}

// CredentialsByKind 过滤出某个平台的凭据, 供上层挑默认值。
func CredentialsByKind(kind string) ([]model.Credential, error) {
	all, err := ListCredentials()
	if err != nil {
		return nil, err
	}
	out := make([]model.Credential, 0, len(all))
	for _, c := range all {
		if c.Kind == kind {
			out = append(out, c)
		}
	}
	return out, nil
}
