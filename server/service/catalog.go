package service

import (
	"fmt"

	"github.com/woodchen-ink/splitdns/server/database"
	"github.com/woodchen-ink/splitdns/server/model"
)

// 回源与凭据都是"被访问域名引用"的基础资料, 增删改查形态一致, 放在同一个文件里维护。

// ListOrigins 列出全部回源。回源数量远小于域名数, 不分页。
func ListOrigins() ([]model.Origin, error) {
	var list []model.Origin
	err := database.DB.Order("name").Find(&list).Error
	return list, err
}

// SaveOrigin 新建或更新一个回源。
func SaveOrigin(o *model.Origin) error {
	if o.Name == "" || o.Value == "" {
		return fmt.Errorf("回源的名称和落点值都不能为空")
	}
	fillCustomOriginSNI(&o.Kind, &o.Value, &o.SNI)
	return database.DB.Save(o).Error
}

// fillCustomOriginSNI 给自定义源补上回源 SNI: 它就是源服务器主机名本身。
//
// CF 回源时默认拿源服务器名字当 SNI, 单独指定别的名字是企业版才有的能力 (非企业账号发了直接被 1456 拒)。
// 留空虽然也能正常回源, 却会让"源站要给这个名字挂 router"这件事从流程里消失 —— 缺了它回源就是 403,
// 所以宁可显式填上, 让 NeedsSNIRoute 认得出来。用户显式填了别的值时不覆盖。
func fillCustomOriginSNI(kind, value, sni *string) {
	if *kind == model.OriginSaaSCustom && *sni == "" {
		*sni = *value
	}
}

// DeleteOrigin 删除回源。仍被线路引用时拒绝删除, 避免留下悬空引用让巡检报一堆假错。
func DeleteOrigin(id uint) error {
	var used int64
	if err := database.DB.Model(&model.Route{}).Where("origin_id = ?", id).Count(&used).Error; err != nil {
		return err
	}
	if used > 0 {
		return fmt.Errorf("该回源还被 %d 条线路使用, 先改掉那些线路再删", used)
	}
	return database.DB.Delete(&model.Origin{}, id).Error
}

// ListCredentials 列出全部凭据。密钥字段带 json:"-", 不会随响应外泄。
func ListCredentials() ([]model.Credential, error) {
	var list []model.Credential
	err := database.DB.Order("kind, name").Find(&list).Error
	return list, err
}

// SaveCredential 新建或更新凭据。
// 更新时留空的密钥字段视为"不修改", 这样前端可以在不回显密钥的前提下改备注名。
func SaveCredential(c *model.Credential) error {
	if c.Name == "" || c.Kind == "" {
		return fmt.Errorf("凭据的名称和平台都不能为空")
	}
	if c.ID != 0 {
		var old model.Credential
		if err := database.DB.First(&old, c.ID).Error; err != nil {
			return fmt.Errorf("读取原凭据失败: %w", err)
		}
		if c.APIToken == "" {
			c.APIToken = old.APIToken
		}
		if c.SecretID == "" {
			c.SecretID = old.SecretID
		}
		if c.SecretKey == "" {
			c.SecretKey = old.SecretKey
		}
	}
	return database.DB.Save(c).Error
}

// DeleteCredential 删除凭据。仍被域名引用时拒绝删除。
func DeleteCredential(id uint) error {
	var used int64
	err := database.DB.Model(&model.Hostname{}).
		Where("cf_credential_id = ? OR saas_credential_id = ? OR dnspod_credential_id = ?", id, id, id).
		Count(&used).Error
	if err != nil {
		return err
	}
	if used > 0 {
		return fmt.Errorf("该凭据还被 %d 个域名使用, 先改掉那些域名再删", used)
	}
	return database.DB.Delete(&model.Credential{}, id).Error
}
