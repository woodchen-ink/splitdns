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
// 父区与它归哪份 CF 凭据管都不让用户填: 拿访问域名去可见 zone 里推导即可, 手打只会多两处填错的地方。
// 没指定凭据时在全部 CF 账号里找, 找到哪个账号就用哪个 —— 域名落在哪个账号下是客观事实, 不该让人再确认一遍。
func SaveHostname(ctx context.Context, h *model.Hostname) error {
	if h.ParentZone == "" || h.CFCredentialID == 0 {
		zone, err := DeriveParentZone(ctx, h.CFCredentialID, h.Hostname)
		if err != nil {
			return err
		}
		h.ParentZone = zone.Zone
		if h.CFCredentialID == 0 {
			h.CFCredentialID = zone.CredentialID
		}
	}
	if h.SaaSZone != "" && sameName(h.SaaSZone, h.ParentZone) {
		return fmt.Errorf("SaaS 区不能就是父区 %s —— 自定义主机名不能是 SaaS 区自己的子域", h.ParentZone)
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
