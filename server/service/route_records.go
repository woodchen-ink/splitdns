package service

import (
	"fmt"
	"net/netip"
	"strings"

	"github.com/woodchen-ink/splitdns/server/model"
)

// 一条线路的落点可以是一个 CNAME, 也可以是一组 IP: IPv4 建 A、IPv6 建 AAAA, 双栈就是同一线路上各一条。
// 巡检、写入、指令三处都按 "线路 × 记录类型" 对账, 不假定一条线路只有一条记录。

// wantRecord 是线路上期望存在的一条落点记录。
type wantRecord struct {
	Type  string
	Value string
}

// recordChange 把已有的一条记录就地改成期望值 (可能连类型一起换, 如 CNAME → A)。
type recordChange struct {
	// Index 指向调用方传入的记录切片, 由调用方换成自己手里的记录 ID
	Index int
	Want  wantRecord
}

// linePlan 是某条线路 "期望 vs 实际" 的对账结果。
type linePlan struct {
	Line string
	Want []wantRecord
	// Matched 值已经对上的记录
	Matched []int
	// Create 缺的记录, 补上不动任何现有解析
	Create []wantRecord
	// Modify 已有但值 / 类型不对的记录, 覆盖的是线上正在生效的解析
	Modify []recordChange
	// Delete 配置里已经没有对应地址的记录 (如去掉了 IPv6 之后残留的 AAAA)
	Delete []int
	// Ambiguous 非空表示同线路同类型有多条记录, 说不清哪条算数, 不自动对账
	Ambiguous string
}

// existing 返回这条线路上实际存在的全部落点记录下标。
func (p linePlan) existing() []int {
	out := append([]int{}, p.Matched...)
	for _, m := range p.Modify {
		out = append(out, m.Index)
	}
	return append(out, p.Delete...)
}

// reshapes 判定这条线路是否要换记录类型或删记录。
// 换类型时新增必须排在改 / 删之后: CNAME 与 A / AAAA 不能同名同线路共存, 先建会被 DNSPod 拒掉,
// 所以这类线路的新增也要跟着确认一起做。
func (p linePlan) reshapes(records []model.DNSRecord) bool {
	if len(p.Delete) > 0 {
		return true
	}
	for _, m := range p.Modify {
		if !strings.EqualFold(records[m.Index].Type, m.Want.Type) {
			return true
		}
	}
	return false
}

// planLine 对账某条线路: 同类型的记录一一对应, 类型对不上的已有记录优先拿去改成缺的那条, 剩下的才删。
// 复用而不是先删后建, 是为了让 "CNAME 换成 IP" 这种改动不出现解析空窗。
func planLine(records []model.DNSRecord, name, line string, values []string) linePlan {
	p := linePlan{Line: line, Want: wantRecords(values)}

	byType := map[string]int{}
	var have []int
	for i, r := range records {
		if !sameName(r.Name, name) || r.Line != line || !isTargetType(r.Type) {
			continue
		}
		t := strings.ToUpper(r.Type)
		if _, dup := byType[t]; dup {
			p.Ambiguous = fmt.Sprintf("线路「%s」下有多条 %s 记录, 说不清该以哪条为准", line, t)
			return p
		}
		byType[t] = i
		have = append(have, i)
	}

	used := map[int]bool{}
	var pending []wantRecord
	for _, w := range p.Want {
		i, ok := byType[w.Type]
		if !ok {
			pending = append(pending, w)
			continue
		}
		if used[i] {
			// 保存时已拦住同族两个地址, 走到这里只可能是存量脏数据, 照样不猜
			p.Ambiguous = fmt.Sprintf("线路「%s」的落点里有多个 %s 地址, 每个协议族只能填一个", line, w.Type)
			return p
		}
		used[i] = true
		if sameTarget(records[i].Value, w.Value) {
			p.Matched = append(p.Matched, i)
		} else {
			p.Modify = append(p.Modify, recordChange{Index: i, Want: w})
		}
	}

	var spare []int
	for _, i := range have {
		if !used[i] {
			spare = append(spare, i)
		}
	}
	for _, w := range pending {
		if len(spare) > 0 {
			p.Modify = append(p.Modify, recordChange{Index: spare[0], Want: w})
			spare = spare[1:]
			continue
		}
		p.Create = append(p.Create, w)
	}
	p.Delete = spare
	return p
}

// wantRecords 把落点值换算成期望记录, 记录类型按值的形态推断。
func wantRecords(values []string) []wantRecord {
	out := make([]wantRecord, 0, len(values))
	for _, v := range values {
		out = append(out, wantRecord{Type: recordTypeFor(v), Value: v})
	}
	return out
}

// isTargetType 判定是不是线路落点会用到的记录类型。
func isTargetType(t string) bool {
	switch strings.ToUpper(t) {
	case "A", "AAAA", "CNAME":
		return true
	}
	return false
}

// recordTypeFor 按落点值形态推断该建什么类型的记录: IPv4 建 A、IPv6 建 AAAA, 其余按主机名建 CNAME。
func recordTypeFor(value string) string {
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return "CNAME"
	}
	if addr.Is4() {
		return "A"
	}
	return "AAAA"
}

// sameTarget 比较两个落点值。IPv6 写法不唯一 (2001:db8::1 与 2001:0db8:0::1 是同一个地址),
// 平台回显的又是规范形式, 两边都是 IP 时按地址比, 否则按主机名比。
func sameTarget(a, b string) bool {
	x, errX := netip.ParseAddr(strings.TrimSpace(a))
	y, errY := netip.ParseAddr(strings.TrimSpace(b))
	if errX == nil && errY == nil {
		return x == y
	}
	return sameName(a, b)
}

// validateTargetValue 校验落点值。单个值保持原样 (主机名或 IP);
// 多个值只允许是 IP, 且每个协议族至多一个 —— 同线路同类型多条记录, 巡检与覆盖都说不清该看哪条。
func validateTargetValue(value string) error {
	values := originAddresses(value)
	if len(values) <= 1 {
		return nil
	}
	seen := map[string]bool{}
	for _, v := range values {
		t := recordTypeFor(v)
		if t == "CNAME" {
			return fmt.Errorf("落点 %q 里的 %s 不是 IP: 填多个值只用于双栈 (一个 IPv4 + 一个 IPv6), CNAME 落点只能单独一个", value, v)
		}
		if seen[t] {
			return fmt.Errorf("落点 %q 里有两个 %s 地址, 每个协议族只能填一个", value, ipFamily(t))
		}
		seen[t] = true
	}
	return nil
}

// validateAddresses 校验源站 IP 一栏: 每项都必须是 IP, 每个协议族至多一个 (代建记录同类型只建一条)。
func validateAddresses(raw string) error {
	seen := map[string]bool{}
	for _, v := range originAddresses(raw) {
		t := recordTypeFor(v)
		if t == "CNAME" {
			return fmt.Errorf("源站 IP 一栏里的 %s 不是 IP 地址", v)
		}
		if seen[t] {
			return fmt.Errorf("源站 IP 里有两个 %s 地址, 每个协议族只能填一个", ipFamily(t))
		}
		seen[t] = true
	}
	return nil
}

func ipFamily(recordType string) string {
	if recordType == "AAAA" {
		return "IPv6"
	}
	return "IPv4"
}

// describeRecord 把一条记录渲染成给人核对的 "类型 值"。
func describeRecord(t, v string) string {
	return strings.ToUpper(t) + " " + v
}

// describeWants 渲染一组期望记录, 供指令与报错文案使用。
func describeWants(wants []wantRecord) string {
	parts := make([]string, 0, len(wants))
	for _, w := range wants {
		parts = append(parts, describeRecord(w.Type, w.Value))
	}
	return strings.Join(parts, " + ")
}
