// updatesign 生成自动更新的签名密钥, 以及在 CI 里给发布产物签名。
//
// 生成密钥 (私钥打到 stdout, 直接管道给 gh secret set, 不落盘):
//
//	go run ./tools/updatesign -keygen -pub ../desktop/updatekey.pub | gh secret set UPDATE_SIGNING_KEY
//
// 签名 (CI 里, 私钥从环境变量 UPDATE_SIGNING_KEY 读):
//
//	go run ./tools/updatesign -dir ../dist -pub ../desktop/updatekey.pub
//
// 签名模式会拿 -pub 回验一遍: secret 与仓库里的公钥对不上时, 发出去的包没有一个客户端装得上,
// 必须在 CI 里就失败, 而不是等用户点了更新才发现。
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/woodchen-ink/splitdns/server/pkg/selfupdate"
)

func main() {
	keygen := flag.Bool("keygen", false, "生成新密钥对: 公钥写 -pub, 私钥打到 stdout")
	dir := flag.String("dir", "", "要签名的发布产物目录")
	pubPath := flag.String("pub", "", "公钥文件路径")
	flag.Parse()

	if *pubPath == "" {
		log.Fatal("必须指定 -pub")
	}
	if *keygen {
		if err := generate(*pubPath); err != nil {
			log.Fatal(err)
		}
		return
	}
	if *dir == "" {
		log.Fatal("必须指定 -dir 或 -keygen")
	}
	if err := sign(*dir, *pubPath); err != nil {
		log.Fatal(err)
	}
}

func generate(pubPath string) error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	if err := os.WriteFile(pubPath, []byte(base64.StdEncoding.EncodeToString(pub)+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Print(base64.StdEncoding.EncodeToString(priv.Seed()))
	fmt.Fprintf(os.Stderr, "公钥已写入 %s, 私钥已输出到 stdout\n", pubPath)
	return nil
}

func sign(dir, pubPath string) error {
	priv, err := selfupdate.ParsePrivateKey(os.Getenv("UPDATE_SIGNING_KEY"))
	if err != nil {
		return fmt.Errorf("读取 UPDATE_SIGNING_KEY 失败: %w", err)
	}
	pubText, err := os.ReadFile(pubPath)
	if err != nil {
		return err
	}
	pub, err := selfupdate.ParsePublicKey(string(pubText))
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var lines []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || name == selfupdate.ChecksumsAsset || name == selfupdate.SignatureAsset {
			continue
		}
		sum, err := fileSHA256(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		lines = append(lines, sum+"  "+name)
	}
	if len(lines) == 0 {
		return fmt.Errorf("%s 里没有可签名的文件", dir)
	}
	slices.Sort(lines)
	sums := []byte(strings.Join(lines, "\n") + "\n")
	sig := selfupdate.Sign(sums, priv)

	if _, err := selfupdate.VerifyChecksums(sums, sig, pub); err != nil {
		return fmt.Errorf("UPDATE_SIGNING_KEY 与 %s 不是一对: %w", pubPath, err)
	}
	if err := os.WriteFile(filepath.Join(dir, selfupdate.ChecksumsAsset), sums, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, selfupdate.SignatureAsset), sig, 0o644); err != nil {
		return err
	}
	fmt.Printf("已签名 %d 个文件:\n%s", len(lines), sums)
	return nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
