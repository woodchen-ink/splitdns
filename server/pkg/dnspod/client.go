package dnspod

import (
	"context"
	"fmt"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	terrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	dnspod "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/dnspod/v20210323"
)

// Client 是腾讯云 DNSPod 3.0 的只读客户端, 一个实例对应一份 SecretId/SecretKey。
type Client struct {
	api *dnspod.Client
}

// New 创建客户端。DNSPod 是全局服务, region 传空即可。
func New(secretID, secretKey string) (*Client, error) {
	cred := common.NewCredential(secretID, secretKey)
	cpf := profile.NewClientProfile()
	cpf.HttpProfile.ReqTimeout = 20
	api, err := dnspod.NewClient(cred, "", cpf)
	if err != nil {
		return nil, fmt.Errorf("创建 DNSPod 客户端失败: %w", err)
	}
	return &Client{api: api}, nil
}

// Domain 是域名基础信息。
type Domain struct {
	// Nameservers DNSPod 实际分配给该域名的 NS, 委派时必须与这组值一致
	Nameservers []string
	// Grade 套餐等级, 决定可用线路数量 (免费版只有 默认/境内/境外)
	Grade string
}

// DescribeDomain 查询域名基础信息。域名不在该账号下时返回明确错误。
func (c *Client) DescribeDomain(ctx context.Context, domain string) (*Domain, error) {
	req := dnspod.NewDescribeDomainRequest()
	req.Domain = common.StringPtr(domain)

	resp, err := c.api.DescribeDomainWithContext(ctx, req)
	if err != nil {
		if sdkErr, ok := err.(*terrors.TencentCloudSDKError); ok {
			return nil, fmt.Errorf("查询域名 %s 失败: %s %s", domain, sdkErr.Code, sdkErr.Message)
		}
		return nil, fmt.Errorf("查询域名 %s 失败: %w", domain, err)
	}
	if resp.Response == nil || resp.Response.DomainInfo == nil {
		return nil, fmt.Errorf("查询域名 %s 返回空结果", domain)
	}

	info := resp.Response.DomainInfo
	d := &Domain{}
	for _, ns := range info.DnspodNsList {
		if ns != nil {
			d.Nameservers = append(d.Nameservers, *ns)
		}
	}
	if info.Grade != nil {
		d.Grade = *info.Grade
	}
	return d, nil
}

// CountDomains 返回该账号下的域名数量, 用来验证凭据可用。
// 拿不到就是凭据无效或权限不足, 错误信息里带上腾讯云的错误码方便对照。
func (c *Client) CountDomains(ctx context.Context) (uint64, error) {
	req := dnspod.NewDescribeDomainListRequest()
	req.Limit = common.Int64Ptr(1)

	resp, err := c.api.DescribeDomainListWithContext(ctx, req)
	if err != nil {
		if sdkErr, ok := err.(*terrors.TencentCloudSDKError); ok {
			return 0, fmt.Errorf("%s %s", sdkErr.Code, sdkErr.Message)
		}
		return 0, err
	}
	if resp.Response == nil || resp.Response.DomainCountInfo == nil || resp.Response.DomainCountInfo.AllTotal == nil {
		return 0, nil
	}
	return *resp.Response.DomainCountInfo.AllTotal, nil
}

// Record 是一条解析记录。
type Record struct {
	Name    string
	Type    string
	Line    string
	Value   string
	TTL     uint64
	Enabled bool
}

// ListRecords 拉取域名下的全部解析记录, 自动翻页。
// 单域名记录数通常很小, 但仍按页拉, 避免大域名被默认页大小截断后误判"记录缺失"。
func (c *Client) ListRecords(ctx context.Context, domain string) ([]Record, error) {
	const pageSize = 100
	var out []Record

	for offset := uint64(0); ; offset += pageSize {
		req := dnspod.NewDescribeRecordListRequest()
		req.Domain = common.StringPtr(domain)
		req.Offset = common.Uint64Ptr(offset)
		req.Limit = common.Uint64Ptr(pageSize)

		resp, err := c.api.DescribeRecordListWithContext(ctx, req)
		if err != nil {
			if sdkErr, ok := err.(*terrors.TencentCloudSDKError); ok {
				// 域名下一条记录都没有时 SDK 会返回该错误码, 这是正常状态不是故障
				if sdkErr.Code == "ResourceNotFound.NoDataOfRecord" {
					return out, nil
				}
				return nil, fmt.Errorf("查询 %s 解析记录失败: %s %s", domain, sdkErr.Code, sdkErr.Message)
			}
			return nil, fmt.Errorf("查询 %s 解析记录失败: %w", domain, err)
		}
		if resp.Response == nil {
			break
		}

		for _, r := range resp.Response.RecordList {
			if r == nil {
				continue
			}
			out = append(out, Record{
				Name:    deref(r.Name),
				Type:    deref(r.Type),
				Line:    deref(r.Line),
				Value:   deref(r.Value),
				TTL:     derefUint(r.TTL),
				Enabled: deref(r.Status) == "ENABLE",
			})
		}

		total := uint64(0)
		if resp.Response.RecordCountInfo != nil && resp.Response.RecordCountInfo.TotalCount != nil {
			total = *resp.Response.RecordCountInfo.TotalCount
		}
		if uint64(len(out)) >= total || len(resp.Response.RecordList) == 0 {
			break
		}
	}
	return out, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefUint(v *uint64) uint64 {
	if v == nil {
		return 0
	}
	return *v
}
