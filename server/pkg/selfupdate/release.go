// Package selfupdate 从 GitHub Releases 取新版本, 并在交给调用方安装之前验明正身。
//
// 仓库是公开的, 读 Release 不需要令牌。但"能从 GitHub 下载"不等于"是我们发的":
// 自动更新等于让程序执行一份下载来的代码, 所以每个安装包都要对上 SHA256SUMS,
// 而 SHA256SUMS 本身要带 ed25519 签名 —— 私钥只在 CI 的 secret 里, 公钥编进二进制。
package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// 校验文件的固定资产名, 与 CI 里 updatesign 工具的产物一致。
const (
	ChecksumsAsset = "SHA256SUMS"
	SignatureAsset = "SHA256SUMS.sig"
)

// Release 是一次正式发布 (不含草稿与预发布: /releases/latest 本身就不返回它们)。
type Release struct {
	Tag         string    `json:"tag"`
	Notes       string    `json:"notes"`
	URL         string    `json:"url"`
	PublishedAt time.Time `json:"publishedAt"`
	Assets      []Asset   `json:"-"`
}

// Asset 是 Release 上挂的一个文件。
type Asset struct {
	Name string
	URL  string
	Size int64
}

// Find 按文件名找资产, 找不到返回 nil。
func (r *Release) Find(name string) *Asset {
	for i := range r.Assets {
		if r.Assets[i].Name == name {
			return &r.Assets[i]
		}
	}
	return nil
}

// httpClient 只管查询接口; 下载安装包走 Download, 那边不能设整体超时 (几十 MB 在慢网上要跑好几分钟)。
var httpClient = &http.Client{Timeout: 20 * time.Second}

// Latest 读仓库的最新正式发布。repo 形如 "owner/name"。
//
// 未认证调用按 IP 限流 60 次 / 小时, 调用方要控制频率, 别挂在前端轮询上。
func Latest(ctx context.Context, repo string) (*Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.github.com/repos/"+repo+"/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	// GitHub 拒绝没有 User-Agent 的请求
	req.Header.Set("User-Agent", "splitdns-updater")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连不上 GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("GitHub 返回 HTTP %d: %s", resp.StatusCode, body)
	}

	var raw struct {
		TagName     string    `json:"tag_name"`
		Body        string    `json:"body"`
		HTMLURL     string    `json:"html_url"`
		PublishedAt time.Time `json:"published_at"`
		Assets      []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
			Size               int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("解析 GitHub 响应失败: %w", err)
	}

	rel := &Release{Tag: raw.TagName, Notes: raw.Body, URL: raw.HTMLURL, PublishedAt: raw.PublishedAt}
	for _, a := range raw.Assets {
		rel.Assets = append(rel.Assets, Asset{Name: a.Name, URL: a.BrowserDownloadURL, Size: a.Size})
	}
	return rel, nil
}
