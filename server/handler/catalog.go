package handler

import (
	"encoding/json"
	"net/http"

	"github.com/woodchen-ink/go-web-utils/resputil"
	"github.com/woodchen-ink/splitdns/server/model"
	"github.com/woodchen-ink/splitdns/server/service"
)

// ListOrigins GET /api/origins
func ListOrigins(w http.ResponseWriter, r *http.Request) {
	list, err := service.ListOrigins()
	if err != nil {
		resputil.Fail(w, 500, err.Error())
		return
	}
	resputil.OK(w, list)
}

// SaveOrigin POST /api/origins
func SaveOrigin(w http.ResponseWriter, r *http.Request) {
	var o model.Origin
	if err := json.NewDecoder(r.Body).Decode(&o); err != nil {
		resputil.Fail(w, 400, "请求体格式错误: "+err.Error())
		return
	}
	if err := service.SaveOrigin(&o); err != nil {
		resputil.Fail(w, 400, err.Error())
		return
	}
	resputil.OK(w, o)
}

// DeleteOrigin DELETE /api/origins/{id}
func DeleteOrigin(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := service.DeleteOrigin(id); err != nil {
		resputil.Fail(w, 400, err.Error())
		return
	}
	resputil.OKMsg(w, nil, "已删除")
}

// ListCredentials GET /api/credentials
func ListCredentials(w http.ResponseWriter, r *http.Request) {
	list, err := service.ListCredentials()
	if err != nil {
		resputil.Fail(w, 500, err.Error())
		return
	}
	resputil.OK(w, list)
}

// credentialInput 是凭据的写入形态。
// model 上的密钥字段是 json:"-", 那个 tag 双向生效: 既挡住响应回显, 也会让请求体里的密钥被丢弃。
// 所以写入必须走这个单独的入参结构, 由它显式接收密钥再映射到 model —— 只写不读。
type credentialInput struct {
	model.Credential
	APIToken  string `json:"apiToken"`
	SecretID  string `json:"secretId"`
	SecretKey string `json:"secretKey"`
}

// SaveCredential POST /api/credentials
func SaveCredential(w http.ResponseWriter, r *http.Request) {
	var in credentialInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		resputil.Fail(w, 400, "请求体格式错误: "+err.Error())
		return
	}
	c := in.Credential
	c.APIToken = in.APIToken
	c.SecretID = in.SecretID
	c.SecretKey = in.SecretKey

	if err := service.SaveCredential(&c); err != nil {
		resputil.Fail(w, 400, err.Error())
		return
	}
	// 回显走 model 本体, 密钥字段仍然被 json:"-" 挡住
	c.HasSecret = c.APIToken != "" || c.SecretKey != ""
	resputil.OK(w, c)
}

// CheckCredential POST /api/credentials/{id}/check 拿凭据真去调一次平台接口
func CheckCredential(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	result, err := service.CheckCredential(r.Context(), id)
	if err != nil {
		resputil.Fail(w, 500, err.Error())
		return
	}
	resputil.OK(w, result)
}

// DeleteCredential DELETE /api/credentials/{id}
func DeleteCredential(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := service.DeleteCredential(id); err != nil {
		resputil.Fail(w, 400, err.Error())
		return
	}
	resputil.OKMsg(w, nil, "已删除")
}
