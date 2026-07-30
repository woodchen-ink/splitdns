package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/woodchen-ink/splitdns/server/model"
)

// 配置里那些要手打的域名, 在平台上本来就存在。这一组接口把它们读出来供界面直接选,
// 少一次手打就少一类"多打一个字母查半天"的问题。

// CFZone 是一个可选的 CF zone 以及它归哪份凭据管。
// 表单里挑完 zone 就能顺带把账号定下来, 不必让用户先选凭据再选区。
type CFZone struct {
	Zone           string `json:"zone"`
	CredentialID   uint   `json:"credentialId"`
	CredentialName string `json:"credentialName"`
}

// CloudflareZones 列出某份 CF 凭据可见的 zone。
// credentialID 为 0 表示不限定凭据, 聚合全部 CF 账号 —— 域名与回源的后缀可能落在任意一个账号下,
// 挑后缀时不该逼用户先选账号。
func CloudflareZones(ctx context.Context, credentialID uint) ([]CFZone, error) {
	if credentialID == 0 {
		return newCFZoneLocator().zoneOptions(ctx)
	}
	var c model.Credential
	if err := credentialRecord(credentialID, &c); err != nil {
		return nil, err
	}
	cf, err := cloudflareClient(credentialID)
	if err != nil {
		return nil, err
	}
	names, err := cf.ListZoneNames(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]CFZone, 0, len(names))
	for _, name := range names {
		out = append(out, CFZone{Zone: normalizeName(name), CredentialID: c.ID, CredentialName: c.Name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Zone < out[j].Zone })
	return out, nil
}

// DeriveParentZone 从访问域名推导它所属的 CF 父区, 并带出该区归哪份凭据管。
//
// 取"可见 zone 里能匹配上的最长后缀": example.com 与 sub.example.com 同时存在时,
// a.sub.example.com 属于更具体的 sub.example.com —— 那才是对它有权威的 zone。
// credentialID 为 0 时在全部 CF 账号里找, 找到哪个账号就用哪个。
func DeriveParentZone(ctx context.Context, credentialID uint, hostname string) (CFZone, error) {
	host := normalizeName(hostname)
	if host == "" {
		return CFZone{}, fmt.Errorf("还没填访问域名")
	}
	zones, err := CloudflareZones(ctx, credentialID)
	if err != nil {
		return CFZone{}, err
	}

	var best CFZone
	for _, z := range zones {
		if z.Zone == "" || (host != z.Zone && !strings.HasSuffix(host, "."+z.Zone)) {
			continue
		}
		if len(z.Zone) > len(best.Zone) {
			best = z
		}
	}
	if best.Zone == "" {
		if credentialID == 0 {
			return CFZone{}, fmt.Errorf("现有的 CF 凭据都看不到 %s 所属的 zone, 确认 Token 的作用范围", hostname)
		}
		return CFZone{}, fmt.Errorf("这份凭据看不到 %s 所属的 zone, 确认 Token 的作用范围", hostname)
	}
	if best.Zone == host {
		return CFZone{}, fmt.Errorf("%s 本身就是一个 CF zone, 这个工具是拿来委派子域名的", hostname)
	}
	return best, nil
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
