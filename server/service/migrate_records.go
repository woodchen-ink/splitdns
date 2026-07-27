package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/woodchen-ink/splitdns/server/model"
	"github.com/woodchen-ink/splitdns/server/pkg/cloudflare"
	"github.com/woodchen-ink/splitdns/server/pkg/dnspod"
)

// migratableTypes 是能原样搬去 DNSPod 的记录类型。
// NS / SOA 属于区本身的元数据, 不该跟着搬; 其余类型两边语义一致。
var migratableTypes = map[string]bool{
	"A": true, "AAAA": true, "CNAME": true, "TXT": true,
	"MX": true, "SRV": true, "CAA": true,
}

// shadowedPlan 是一条被遮蔽记录的处置方案。
type shadowedPlan struct {
	Record cloudflare.DNSRecord
	// SubDomain 搬到 DNSPod 后的主机记录; 不可搬时为空
	SubDomain string
	// Skip 不搬的原因, 为空表示会搬
	Skip string
}

// planShadowedMigration 为每条被遮蔽记录定好处置方式。
//
// 被遮蔽不等于该扔: 这些记录本来在正常服务, 只是委派之后它们待错了地方。
// 直接删会把线上打断, 所以先搬到 DNSPod 再删。
func planShadowedMigration(h model.Hostname, shadowed []cloudflare.DNSRecord, existing []model.DNSRecord) []shadowedPlan {
	zone := h.DNSPodZone()

	// DNSPod 上已有的同名同类型记录不重复建, 免得一条名字挂两份值
	have := map[string]bool{}
	for _, r := range existing {
		have[strings.ToLower(r.Name)+"|"+r.Type] = true
	}

	out := make([]shadowedPlan, 0, len(shadowed))
	for _, r := range shadowed {
		p := shadowedPlan{Record: r}
		sub := relativeName(r.Name, zone)

		switch {
		case sub == "@":
			// 这条就是接入域名本身。它该指向哪由「配置分线路解析记录」那一步决定,
			// 把旧值搬过去只会和线路记录打架
			p.Skip = "这条就是接入域名本身, 落点归线路配置管, 旧值直接删掉"
		case isManagedTXT(sub):
			p.Skip = "验证记录由流程自己维护, 不用搬"
		case !migratableTypes[r.Type]:
			p.Skip = r.Type + " 记录属于区本身的元数据, 不搬"
		case have[strings.ToLower(sub)+"|"+r.Type]:
			p.Skip = "DNSPod 上已有同名同类型记录"
		default:
			p.SubDomain = sub
		}
		out = append(out, p)
	}
	return out
}

// isManagedTXT 判断这条是不是流程自己会写的验证记录。
// 这类记录搬过去没有意义: 值随每次签发变化, 流程会按当前要求重新写。
func isManagedTXT(sub string) bool {
	s := strings.ToLower(sub)
	return strings.HasPrefix(s, "_acme-challenge") || strings.HasPrefix(s, "_cf-custom-hostname")
}

// describeShadowedPlan 把处置方案渲染成给人看的清单。
func describeShadowedPlan(plans []shadowedPlan, zone string) string {
	var b strings.Builder
	for _, p := range plans {
		if p.Skip != "" {
			fmt.Fprintf(&b, "  跳过  %s %s —— %s\n", p.Record.Name, p.Record.Type, p.Skip)
			continue
		}
		fmt.Fprintf(&b, "  搬走  %s %s %s → %s 的 %s\n",
			p.Record.Name, p.Record.Type, p.Record.Content, zone, p.SubDomain)
		if p.Record.Proxied {
			b.WriteString("        ⚠ 这条原本是橙云(经过 CF 代理), 搬到 DNSPod 后就是直连了\n")
		}
	}
	return b.String()
}

// migrateShadowedRecords 把能搬的记录写进 DNSPod, 返回实际搬走的条数。
func migrateShadowedRecords(ctx context.Context, dp *dnspod.Client, zone string, plans []shadowedPlan) (int, error) {
	moved := 0
	for _, p := range plans {
		if p.Skip != "" {
			continue
		}
		ttl := uint64(p.Record.TTL)
		// CF 的 TTL=1 表示自动, 换成 DNSPod 能接受的值; 免费版低于 600 会被拒
		if ttl < dnspod.DefaultTTL {
			ttl = dnspod.DefaultTTL
		}
		var priority *uint64
		if p.Record.Priority != nil {
			v := uint64(*p.Record.Priority)
			priority = &v
		}

		err := dp.CreateRecord(ctx, zone, dnspod.NewRecord{
			SubDomain: p.SubDomain,
			Type:      p.Record.Type,
			Line:      defaultLine,
			Value:     p.Record.Content,
			TTL:       ttl,
			Priority:  priority,
		})
		if err != nil {
			return moved, fmt.Errorf("搬 %s 到 DNSPod 失败: %w", p.Record.Name, err)
		}
		moved++
	}
	return moved, nil
}
