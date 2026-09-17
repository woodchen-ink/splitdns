package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// downloadClient 不设整体超时 (大文件在慢网上要跑很久), 靠 ctx 取消 + 连接级超时兜住卡死。
var downloadClient = &http.Client{
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	},
}

// maxSmallFile 是校验文件的读取上限, 防一个被替换成巨型文件的地址把内存吃满。
const maxSmallFile = 64 << 10

// Fetch 读一个小文件 (校验和、签名) 进内存。
func Fetch(ctx context.Context, url string) ([]byte, error) {
	resp, err := get(ctx, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, maxSmallFile))
}

// Download 把 url 写到 dst, 边写边算 sha256, 返回小写十六进制摘要。
// onProgress 每写一块回调一次, total 取不到时为 -1。失败时删掉写了一半的文件。
func Download(ctx context.Context, url, dst string, onProgress func(received, total int64)) (string, error) {
	resp, err := get(ctx, url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	pw := &progressWriter{total: resp.ContentLength, onProgress: onProgress}
	_, err = io.Copy(io.MultiWriter(f, h, pw), resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(dst)
		return "", fmt.Errorf("下载中断: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "splitdns-updater")
	resp, err := downloadClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

type progressWriter struct {
	received   int64
	total      int64
	onProgress func(received, total int64)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.received += int64(len(b))
	if p.onProgress != nil {
		p.onProgress(p.received, p.total)
	}
	return len(b), nil
}
