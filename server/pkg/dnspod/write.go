package dnspod

import (
	"context"
	"fmt"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	terrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	dnspod "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/dnspod/v20210323"
)

// CreateDomain 在 DNSPod 添加域名。域名已存在时视为成功 —— 这一步要幂等,
// 用户可能已经手动加过, 不该因此卡住整个流程。
func (c *Client) CreateDomain(ctx context.Context, domain string) error {
	req := dnspod.NewCreateDomainRequest()
	req.Domain = common.StringPtr(domain)

	if _, err := c.api.CreateDomainWithContext(ctx, req); err != nil {
		if sdkErr, ok := err.(*terrors.TencentCloudSDKError); ok {
			if sdkErr.Code == "InvalidParameter.DomainExists" || sdkErr.Code == "InvalidParameter.DomainIsAliasDomain" {
				return nil
			}
			return fmt.Errorf("添加域名 %s 失败: %s %s", domain, sdkErr.Code, sdkErr.Message)
		}
		return fmt.Errorf("添加域名 %s 失败: %w", domain, err)
	}
	return nil
}

// NewRecord 是创建解析记录的入参。SubDomain 用相对主机记录写法, 与控制台一致。
type NewRecord struct {
	SubDomain string
	Type      string
	Line      string
	Value     string
	TTL       uint64
}

// CreateRecord 添加一条解析记录。TTL 默认 120 秒, 切换期间好回滚。
func (c *Client) CreateRecord(ctx context.Context, domain string, r NewRecord) error {
	if r.TTL == 0 {
		r.TTL = 120
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
