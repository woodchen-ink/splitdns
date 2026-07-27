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

// SaveCredential POST /api/credentials
func SaveCredential(w http.ResponseWriter, r *http.Request) {
	var c model.Credential
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		resputil.Fail(w, 400, "请求体格式错误: "+err.Error())
		return
	}
	if err := service.SaveCredential(&c); err != nil {
		resputil.Fail(w, 400, err.Error())
		return
	}
	resputil.OK(w, c)
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
