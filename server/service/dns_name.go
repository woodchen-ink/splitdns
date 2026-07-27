package service

import "strings"

// relativeName 把绝对主机名转成相对某个区的主机记录写法, 与 DNSPod 控制台一致。
// img.example.com 相对 img.example.com 得到 "@"; _acme-challenge.img.example.com 得到 "_acme-challenge"。
// 不属于该区时原样返回, 由调用方判定为异常。
func relativeName(fqdn, zone string) string {
	f := normalizeName(fqdn)
	z := normalizeName(zone)
	if f == z {
		return "@"
	}
	if strings.HasSuffix(f, "."+z) {
		return strings.TrimSuffix(f, "."+z)
	}
	return f
}

// normalizeName 统一大小写与结尾的点, 便于比较。
// DNS 名大小写不敏感, CF 与 DNSPod 对结尾点的处理也不一致, 比较前必须先归一。
func normalizeName(s string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(s), "."))
}

// sameName 判定两个 DNS 名是否指向同一个名字。
func sameName(a, b string) bool {
	return normalizeName(a) == normalizeName(b)
}
