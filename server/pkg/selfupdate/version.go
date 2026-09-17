package selfupdate

import (
	"strconv"
	"strings"
)

// ParseVersion 解析 vMAJOR.MINOR.PATCH (v 可省)。带后缀的 (dev-xxx、1.2.3-rc1) 一律不认:
// 本地构建和预发布都不该参与自动更新。
func ParseVersion(s string) ([3]int, bool) {
	var v [3]int
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(s), "v"), ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

// Newer 判断 latest 是否比 current 新。任一方解析不了都返回 false —— 说不清就不更新。
func Newer(latest, current string) bool {
	l, ok1 := ParseVersion(latest)
	c, ok2 := ParseVersion(current)
	if !ok1 || !ok2 {
		return false
	}
	for i := range l {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}
