package dnspod

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
	// Status 原始状态值 (ENABLE / LOCK / PAUSE / SPAM), 判错时靠它对照
	Status string
	// Enabled 解析是否在对外生效
	Enabled bool
	// DNSStatus DNSPod 对"域名 NS 是否指向自己"的周期性检测结论:
	// DNS_ERROR 表示未指向, 空表示正常或还没检测。只能拿它的报错当信号, 不能拿空值当"正常"
	DNSStatus string
}

// 只有这两个状态是真的不解析。用黑名单而不是"只认 ENABLE"的白名单:
// 腾讯云返回大小写不总一致, 也可能出现文档外的取值, 白名单会把它们全误判成暂停。
var pausedStatuses = map[string]bool{"PAUSE": true, "SPAM": true}

// ErrDomainNotFound 表示这个腾讯云账号下查不到该域名 —— 还没添加, 或者已经删掉。
// 单独给一个哨兵值是因为拆除流程要靠它确认域名真的没了,
// 把网络错误、权限错误一起当成"已删除"会让人以为清干净了。
var ErrDomainNotFound = errors.New("DNSPod 上查不到这个域名")

// domainNotFoundCodes 是"域名不存在"在腾讯云的几种写法。
// 同一语义会挂在不同前缀下 (InvalidParameter. / ResourceNotFound. ...), 所以按后缀匹配。
var domainNotFoundCodes = []string{"NoDataOfDomain", "DomainNotExists", "DomainIsNotExist"}

// isDomainNotFound 判定这个错误码是不是"域名不存在"。
func isDomainNotFound(code string) bool {
	for _, suffix := range domainNotFoundCodes {
		if strings.Contains(code, suffix) {
			return true
		}
	}
	return false
}

// wrapDomainErr 把腾讯云的报错转成带上下文的 error, 域名不存在时统一成 ErrDomainNotFound。
func wrapDomainErr(action, domain string, err error) error {
	sdkErr, ok := err.(*terrors.TencentCloudSDKError)
	if !ok {
		return fmt.Errorf("%s %s 失败: %w", action, domain, err)
	}
	if isDomainNotFound(sdkErr.Code) {
		return fmt.Errorf("%s %s: %w", action, domain, ErrDomainNotFound)
	}
	return fmt.Errorf("%s %s 失败: %s %s", action, domain, sdkErr.Code, sdkErr.Message)
}

// DescribeDomain 查询域名基础信息。域名不在该账号下时返回 ErrDomainNotFound。
func (c *Client) DescribeDomain(ctx context.Context, domain string) (*Domain, error) {
	req := dnspod.NewDescribeDomainRequest()
	req.Domain = common.StringPtr(domain)

	resp, err := c.api.DescribeDomainWithContext(ctx, req)
	if err != nil {
		return nil, wrapDomainErr("查询域名", domain, err)
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
	d.Status = deref(info.Status)
	d.Enabled = !pausedStatuses[strings.ToUpper(d.Status)]
	d.DNSStatus = deref(info.DnsStatus)
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

// ListDomainNames 列出该账号下的域名, 供界面直接选而不是手打。
func (c *Client) ListDomainNames(ctx context.Context) ([]string, error) {
	req := dnspod.NewDescribeDomainListRequest()
	req.Limit = common.Int64Ptr(3000)

	resp, err := c.api.DescribeDomainListWithContext(ctx, req)
	if err != nil {
		if sdkErr, ok := err.(*terrors.TencentCloudSDKError); ok {
			return nil, fmt.Errorf("%s %s", sdkErr.Code, sdkErr.Message)
		}
		return nil, err
	}
	if resp.Response == nil {
		return nil, nil
	}
	names := make([]string, 0, len(resp.Response.DomainList))
	for _, d := range resp.Response.DomainList {
		if d != nil && d.Name != nil {
			names = append(names, *d.Name)
		}
	}
	return names, nil
}

// Record 是一条解析记录。
type Record struct {
	// ID 删除记录时唯一的定位方式, 同名同类型可以有多条 (不同线路)
	ID      uint64
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
			}
			return nil, wrapDomainErr("查询解析记录", domain, err)
		}
		if resp.Response == nil {
			break
		}

		for _, r := range resp.Response.RecordList {
			if r == nil {
				continue
			}
			out = append(out, Record{
				ID:      derefUint(r.RecordId),
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
