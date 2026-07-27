package dnspod

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	terrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	dnspod "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/dnspod/v20210323"
)

// ErrNeedOwnershipTXT 表示 DNSPod 要求先验证域名归属才允许添加。
// 调用方拿到它之后应该去取验证 TXT、写进主域名, 再重试。
var ErrNeedOwnershipTXT = errors.New("需要先验证域名归属")

// CreateDomain 在 DNSPod 添加域名。域名已存在时视为成功 —— 这一步要幂等,
// 用户可能已经手动加过, 不该因此卡住整个流程。
func (c *Client) CreateDomain(ctx context.Context, domain string) error {
	req := dnspod.NewCreateDomainRequest()
	req.Domain = common.StringPtr(domain)

	if _, err := c.api.CreateDomainWithContext(ctx, req); err != nil {
		if sdkErr, ok := err.(*terrors.TencentCloudSDKError); ok {
			// 腾讯云同一个语义会挂在不同前缀下 (InvalidParameter. / FailedOperation. ...),
			// 所以按后缀判定, 不硬编码整串错误码
			switch {
			case strings.Contains(sdkErr.Code, "DomainExists"),
				strings.Contains(sdkErr.Code, "DomainIsAliasDomain"):
				// 已经加过了, 对幂等流程来说就是成功
				return nil
			case strings.Contains(sdkErr.Code, "Quhui"), strings.Contains(sdkErr.Code, "Verif"):
				// 主域名不在这个腾讯云账号下时, 添加子域名要先证明归属
				return fmt.Errorf("%w: %s", ErrNeedOwnershipTXT, sdkErr.Message)
			}
			return fmt.Errorf("添加域名 %s 失败: %s %s", domain, sdkErr.Code, sdkErr.Message)
		}
		return fmt.Errorf("添加域名 %s 失败: %w", domain, err)
	}
	return nil
}

// EnableDomain 启用域名解析。
// DNSPod 新加的域名默认是暂停状态, 不开的话解析记录配得再对也不会生效。
func (c *Client) EnableDomain(ctx context.Context, domain string) error {
	req := dnspod.NewModifyDomainStatusRequest()
	req.Domain = common.StringPtr(domain)
	req.Status = common.StringPtr("enable")

	if _, err := c.api.ModifyDomainStatusWithContext(ctx, req); err != nil {
		if sdkErr, ok := err.(*terrors.TencentCloudSDKError); ok {
			return fmt.Errorf("启用 %s 的解析失败: %s %s", domain, sdkErr.Code, sdkErr.Message)
		}
		return fmt.Errorf("启用 %s 的解析失败: %w", domain, err)
	}
	return nil
}

// OwnershipTXT 是 DNSPod 要求用来证明域名归属的 TXT 记录。
// Domain 是要加记录的主域名, FQDN 是完整记录名。
type OwnershipTXT struct {
	Domain string
	FQDN   string
	Value  string
}

// SubdomainOwnershipTXT 取添加子域名所需的归属验证 TXT。
func (c *Client) SubdomainOwnershipTXT(ctx context.Context, zone string) (*OwnershipTXT, error) {
	req := dnspod.NewCreateSubdomainValidateTXTValueRequest()
	req.DomainZone = common.StringPtr(zone)

	resp, err := c.api.CreateSubdomainValidateTXTValueWithContext(ctx, req)
	if err != nil {
		if sdkErr, ok := err.(*terrors.TencentCloudSDKError); ok {
			return nil, fmt.Errorf("获取 %s 的归属验证 TXT 失败: %s %s", zone, sdkErr.Code, sdkErr.Message)
		}
		return nil, fmt.Errorf("获取 %s 的归属验证 TXT 失败: %w", zone, err)
	}
	if resp.Response == nil {
		return nil, fmt.Errorf("获取 %s 的归属验证 TXT 返回空结果", zone)
	}

	r := resp.Response
	sub := deref(r.SubDomain)
	if sub == "" {
		sub = deref(r.Subdomain)
	}
	domain := deref(r.Domain)
	if domain == "" || sub == "" || deref(r.Value) == "" {
		return nil, fmt.Errorf("DNSPod 没给出完整的验证记录 (域名 %q 主机记录 %q)", domain, sub)
	}
	return &OwnershipTXT{
		Domain: domain,
		FQDN:   sub + "." + domain,
		Value:  deref(r.Value),
	}, nil
}

// NewRecord 是创建解析记录的入参。SubDomain 用相对主机记录写法, 与控制台一致。
type NewRecord struct {
	SubDomain string
	Type      string
	Line      string
	Value     string
	TTL       uint64
}

// DefaultTTL 是新建记录用的 TTL。
// DNSPod 免费版最低只能到 600 秒, 填更小的值会被接口直接拒掉。
const DefaultTTL = 600

// CreateRecord 添加一条解析记录。
func (c *Client) CreateRecord(ctx context.Context, domain string, r NewRecord) error {
	if r.TTL == 0 {
		r.TTL = DefaultTTL
	}
	req := dnspod.NewCreateRecordRequest()
	req.Domain = common.StringPtr(domain)
	req.SubDomain = common.StringPtr(r.SubDomain)
	req.RecordType = common.StringPtr(r.Type)
	req.RecordLine = common.StringPtr(r.Line)
	req.Value = common.StringPtr(r.Value)
	req.TTL = common.Uint64Ptr(r.TTL)

	if _, err := c.api.CreateRecordWithContext(ctx, req); err != nil {
		if sdkErr, ok := err.(*terrors.TencentCloudSDKError); ok {
			// 同名同线路同值的记录已存在时 DNSPod 直接报错, 对幂等流程来说这是成功
			if sdkErr.Code == "InvalidParameter.RecordExists" {
				return nil
			}
			return fmt.Errorf("添加记录 %s.%s (%s) 失败: %s %s", r.SubDomain, domain, r.Line, sdkErr.Code, sdkErr.Message)
		}
		return fmt.Errorf("添加记录 %s.%s (%s) 失败: %w", r.SubDomain, domain, r.Line, err)
	}
	return nil
}
