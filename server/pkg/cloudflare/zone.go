package cloudflare

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// zone 是 zone 列表接口需要的最小字段集。
type zone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ZoneIDByName 按 zone 名查 zone ID。查不到时返回明确错误, 不返回空字符串让调用方猜。
func (c *Client) ZoneIDByName(ctx context.Context, name string) (string, error) {
	var zones []zone
	q := url.Values{"name": {name}}
	if err := c.get(ctx, "/zones", q, &zones); err != nil {
		return "", err
	}
	for _, z := range zones {
		if z.Name == name {
			return z.ID, nil
		}
	}
	return "", fmt.Errorf("在当前 token 可见范围内找不到 zone %s", name)
}

// DNSRecord 是 DNS 记录接口返回的字段子集, 含 shadow 元数据。
type DNSRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
	// TTL 1 表示自动
	TTL int `json:"ttl"`
	// Priority 仅 MX / SRV 有值
	Priority *int `json:"priority"`
	// Comment 记录备注, 本工具建的记录都会写上来源
	Comment string `json:"comment"`
}

// ListRecords 按名字 (可选类型) 查记录。name 传完整主机名, 不是相对名。
func (c *Client) ListRecords(ctx context.Context, zoneID, name, recordType string) ([]DNSRecord, error) {
	q := url.Values{"per_page": {"100"}}
	if name != "" {
		q.Set("name", name)
	}
	if recordType != "" {
		q.Set("type", recordType)
	}
	var records []DNSRecord
	if err := c.get(ctx, "/zones/"+zoneID+"/dns_records", q, &records); err != nil {
		return nil, err
	}
	return records, nil
}

// RecordsByComment 按备注前缀查记录。
// 本工具写进 CF 的记录名字和类型各不相同, 只有备注是稳定的, 拆除时靠它认出自己留下的痕迹。
func (c *Client) RecordsByComment(ctx context.Context, zoneID, prefix string) ([]DNSRecord, error) {
	q := url.Values{"per_page": {"100"}, "comment.startswith": {prefix}}
	var records []DNSRecord
	if err := c.get(ctx, "/zones/"+zoneID+"/dns_records", q, &records); err != nil {
		return nil, err
	}
	// CF 若不认这个过滤参数就会把整个区的记录全返回, 本地再筛一遍,
	// 免得把别人的记录当成本工具的痕迹删掉
	out := make([]DNSRecord, 0, len(records))
	for _, r := range records {
		if strings.HasPrefix(r.Comment, prefix) {
			out = append(out, r)
		}
	}
	return out, nil
}

// ShadowedRecords 返回被指定委派点遮蔽的记录。
// 这些记录在面板里仍然可见但一条都不生效, 是委派后最容易误判的坑。
func (c *Client) ShadowedRecords(ctx context.Context, zoneID, delegationName string) ([]DNSRecord, error) {
	q := url.Values{
		"include_shadow_metadata": {"true"},
		"shadowed_by_name":        {delegationName},
		"per_page":                {"100"},
	}
	var records []DNSRecord
	if err := c.get(ctx, "/zones/"+zoneID+"/dns_records", q, &records); err != nil {
		return nil, err
	}
	return records, nil
}
