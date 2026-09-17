package selfupdate

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v1.7.0", "v1.6.0", true},
		{"v1.10.0", "v1.9.3", true},
		{"v1.6.0", "v1.6.0", false},
		{"v1.5.9", "v1.6.0", false},
		{"v2.0.0", "dev", false},
		{"v1.7.0-rc1", "v1.6.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.latest, c.current); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}

func TestVerifyChecksums(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	seed, err := ParsePrivateKey(base64.StdEncoding.EncodeToString(priv.Seed()))
	if err != nil {
		t.Fatal(err)
	}
	sums := []byte("ABCDEF  splitdns-v1.7.0-windows-amd64-installer.exe\nabc123 *other.zip\n")
	sig := Sign(sums, seed)

	got, err := VerifyChecksums(sums, sig, pub)
	if err != nil {
		t.Fatal(err)
	}
	if got["splitdns-v1.7.0-windows-amd64-installer.exe"] != "abcdef" || got["other.zip"] != "abc123" {
		t.Fatalf("解析结果不对: %v", got)
	}

	tampered := append([]byte{}, sums...)
	tampered[0] = '0'
	if _, err := VerifyChecksums(tampered, sig, pub); err == nil {
		t.Fatal("篡改过的校验文件不该通过")
	}

	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := VerifyChecksums(sums, sig, otherPub); err == nil {
		t.Fatal("别的公钥不该验得过")
	}
}
