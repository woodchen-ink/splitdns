package service

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/woodchen-ink/splitdns/server/pkg/selfupdate"
)

// fakeRelease 起一个本地服务模拟 GitHub Release 的资产下载。
func fakeRelease(t *testing.T, files map[string][]byte) *selfupdate.Release {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := files[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	rel := &selfupdate.Release{Tag: "v9.9.9"}
	for name, body := range files {
		rel.Assets = append(rel.Assets, selfupdate.Asset{Name: name, URL: srv.URL + "/" + name, Size: int64(len(body))})
	}
	return rel
}

func signedFiles(priv ed25519.PrivateKey, pkgName string, pkg []byte) map[string][]byte {
	sum := sha256.Sum256(pkg)
	sums := []byte(hex.EncodeToString(sum[:]) + "  " + pkgName + "\n")
	return map[string][]byte{
		pkgName:                   pkg,
		selfupdate.ChecksumsAsset: sums,
		selfupdate.SignatureAsset: selfupdate.Sign(sums, priv),
	}
}

func TestDownloadVerified(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	const name = "splitdns-v9.9.9-windows-amd64-installer.exe"
	pkg := []byte("installer bytes")
	opts := UpdateOptions{
		PublicKey:   pub,
		AssetName:   func(string) string { return name },
		DownloadDir: filepath.Join(t.TempDir(), "updates"),
	}

	t.Run("签名与摘要都对得上", func(t *testing.T) {
		file, err := downloadVerified(context.Background(), opts, fakeRelease(t, signedFiles(priv, name, pkg)))
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := os.ReadFile(file); string(got) != string(pkg) {
			t.Fatalf("落盘内容不对: %q", got)
		}
	})

	t.Run("安装包被替换", func(t *testing.T) {
		files := signedFiles(priv, name, pkg)
		files[name] = []byte("evil bytes")
		_, err := downloadVerified(context.Background(), opts, fakeRelease(t, files))
		if err == nil {
			t.Fatal("摘要对不上的包不该通过")
		}
		if _, statErr := os.Stat(filepath.Join(opts.DownloadDir, name)); statErr == nil {
			t.Fatal("校验失败的包不该留在磁盘上")
		}
	})

	t.Run("清单用别的私钥签", func(t *testing.T) {
		_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
		_, err := downloadVerified(context.Background(), opts, fakeRelease(t, signedFiles(otherPriv, name, pkg)))
		if err == nil {
			t.Fatal("签名不对的清单不该通过")
		}
	})
}
