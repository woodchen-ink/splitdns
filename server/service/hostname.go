package service

import (
	"context"
	"fmt"

	"github.com/woodchen-ink/splitdns/server/database"
	"github.com/woodchen-ink/splitdns/server/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// HostnameByID 读单个访问域名, 连带线路与线路指向的回源一起预加载,
// 让上层拿到的就是可直接判定的完整对象, 不用再回查。
func HostnameByID(id uint) (*model.Hostname, error) {
	var h model.Hostname
	err := database.DB.
		Preload("Routes", func(db *gorm.DB) *gorm.DB { return db.Order("line") }).
		Preload("Routes.Origin").
		First(&h, id).Error
	if err != nil {
		return nil, fmt.Errorf("读取域名 %d 失败: %w", id, err)
	}
	return &h, nil
}

// HostnameListItem 是列表页的视图模型: 域名本体 + 各条流程的完成度。
// 完成度由后端算好, 前端只渲染 —— "skipped 也算走完"这类口径只该有一处。
type HostnameListItem struct {
	model.Hostname
	Plans []PlanProgress `json:"plans"`
}

// ListHostnames 按关键字分页列出访问域名, 连带每个域名的流程完成度。
// 域名多起来之后列表必须能搜, 不做全量返回。
func ListHostnames(keyword string, offset, limit int) ([]HostnameListItem, int64, error) {
	q := database.DB.Model(&model.Hostname{})
	if keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("hostname LIKE ? OR parent_zone LIKE ? OR note LIKE ?", like, like, like)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	var list []model.Hostname
	err := q.Preload("Routes").Preload("Routes.Origin").
		Order("hostname").Offset(offset).Limit(limit).Find(&list).Error
	if err != nil {
		return nil, 0, err
	}

	ids := make([]uint, 0, len(list))
	for _, h := range list {
		ids = append(ids, h.ID)
	}
	progress, err := planProgressByHostname(ids)
	if err != nil {
		return nil, 0, err
	}
	items := make([]HostnameListItem, 0, len(list))
	for _, h := range list {
		items = append(items, HostnameListItem{Hostname: h, Plans: progress[h.ID]})
	}
	return items, total, nil
}

// SaveHostname 新建或更新访问域名, 连同线路一起整体替换。
// 线路是从属于域名的配置项, 整体替换比逐条 diff 简单可靠, 数量也就几条。
//
// direct 为真表示直托模式: 域名的权威 DNS 本来就在 DNSPod, 没有 CF 父区。
// 空 ParentZone 在两种模式下含义不同 (委派模式是"待推导", 直托模式是"就该为空"),
// 所以模式必须由调用方显式声明, 不能拿字段值反推。
//
// 归属类字段都不让用户手打: 委派模式推导父区与 CF 凭据, 直托模式推导 DNSPod 域名与 SaaS 凭据 ——
// 域名落在哪个账号 / 哪个区下是客观事实, 不该让人再确认一遍。
func SaveHostname(ctx context.Context, h *model.Hostname, direct bool) error {
	if direct {
		if err := fillDirectHostname(ctx, h); err != nil {
			return err
		}
	} else if h.ParentZone == "" || h.CFCredentialID == 0 {
		zone, err := DeriveParentZone(ctx, h.CFCredentialID, h.Hostname)
		if err != nil {
			return err
		}
		h.ParentZone = zone.Zone
		if h.CFCredentialID == 0 {
			h.CFCredentialID = zone.CredentialID
		}
	}
	// 委派模式下父区必然管辖访问域名, 这条同时覆盖了旧的 "SaaS 区 == 父区" 校验
	if h.SaaSZone != "" && inZone(h.Hostname, h.SaaSZone) {
		return fmt.Errorf("SaaS 区 %s 管辖着 %s —— 自定义主机名不能是 SaaS 区自己的子域", h.SaaSZone, h.Hostname)
	}

	return database.DB.Transaction(func(tx *gorm.DB) error {
		if h.ID != 0 {
			if err := tx.Where("hostname_id = ?", h.ID).Delete(&model.Route{}).Error; err != nil {
				return err
			}
		}
		// 线路里带着预加载的 Origin, 直接 Save 会连带更新回源本体, 这里显式切断关联写入
		for i := range h.Routes {
			h.Routes[i].Origin = nil
			h.Routes[i].ID = 0
			// 内联落点与回源库里的条目同一套规则, 自定义源的 SNI 就是落点值本身
			if h.Routes[i].OriginID == 0 {
				fillCustomOriginSNI(&h.Routes[i].Kind, &h.Routes[i].Value, &h.Routes[i].SNI)
			}
		}
		return tx.Session(&gorm.Session{FullSaveAssociations: false}).
			Clauses(clause.OnConflict{UpdateAll: true}).
			Save(h).Error
	})
}

// fillDirectHostname 补齐直托模式下的归属字段。
//
// ParentZone / CFCredentialID 强制清空 —— 前端切换模式后残留的旧值不该跟着存进去。
// DNSPod 域名从凭据可见的域名列表里按最长后缀推导 (判据同 DeriveParentZone),
// 顺带验证了"这个域名确实托管在这个 DNSPod 账号下"这一模式前提。
// SaaS 凭据没有父区凭据可回落, 必须在这里定位到管辖 SaaS 区的那份。
func fillDirectHostname(ctx context.Context, h *model.Hostname) error {
	h.ParentZone = ""
	h.CFCredentialID = 0

	if h.DNSPodDomain == "" || !inZone(h.Hostname, h.DNSPodDomain) {
		zone, err := deriveDNSPodDomain(ctx, h.DNSPodCredentialID, h.Hostname)
		if err != nil {
			return err
		}
		h.DNSPodDomain = zone
	}

	if h.SaaSZone != "" && h.SaaSCredentialID == 0 {
		z, err := newCFZoneLocator().locate(ctx, h.SaaSZone)
		if err != nil {
			return fmt.Errorf("定位 SaaS 区 %s 的凭据失败: %w", h.SaaSZone, err)
		}
		if z == nil {
			return fmt.Errorf("现有的 CF 凭据都看不到 SaaS 区 %s, 确认 Token 的作用范围", h.SaaSZone)
		}
		h.SaaSCredentialID = z.credentialID
	}
	return nil
}

// deriveDNSPodDomain 从凭据可见的 DNSPod 域名里推导管辖该主机名的域名 (最长后缀匹配)。
func deriveDNSPodDomain(ctx context.Context, credentialID uint, hostname string) (string, error) {
	if credentialID == 0 {
		return "", fmt.Errorf("直托模式要先选 DNSPod 凭据, 才能确认 %s 托管在哪个域名下", hostname)
	}
	names, err := DNSPodDomains(ctx, credentialID)
	if err != nil {
		return "", fmt.Errorf("读取 DNSPod 域名列表失败: %w", err)
	}
	best := ""
	for _, name := range names {
		n := normalizeName(name)
		if inZone(hostname, n) && len(n) > len(best) {
			best = n
		}
	}
	if best == "" {
		return "", fmt.Errorf("这份腾讯云凭据下没有托管 %s 的域名 —— 直托模式要求根域名已经加进 DNSPod", hostname)
	}
	return best, nil
}

// DeleteHostname 删除访问域名及其线路与流程。只动本工具的数据库, 不碰任何平台上的真实解析。
func DeleteHostname(id uint) error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("hostname_id = ?", id).Delete(&model.Route{}).Error; err != nil {
			return err
		}
		var plans []model.Plan
		if err := tx.Where("hostname_id = ?", id).Find(&plans).Error; err != nil {
			return err
		}
		for _, p := range plans {
			if err := tx.Where("plan_id = ?", p.ID).Delete(&model.Step{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("hostname_id = ?", id).Delete(&model.Plan{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.Hostname{}, id).Error
	})
}
