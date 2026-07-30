package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/woodchen-ink/splitdns/server/database"
	"github.com/woodchen-ink/splitdns/server/model"
	"github.com/woodchen-ink/splitdns/server/pkg/cloudflare"
)

// 回源的落点如果是个主机名, CF 上必须真有一条橙云记录指向源站, 流量才落得下去。
// CF for SaaS 对自定义源服务器写得很明确: 必须是本账号 DNS 里一条橙云的 A/AAAA/CNAME 记录,
// 不能直接填 IP。没建或者建成灰云都会让回源直接失败, 而自定义主机名页面上一点异常都看不出来。
//
// 回源是可被多个访问域名共用的公共资料, 上面没有登记凭据与 zone, 所以这里按
// "哪份 CF 凭据看得见管辖这个名字的 zone" 反查, 判据与 DeriveParentZone 一致: 取最长后缀匹配。

// commentOriginRecord 本工具代建的落点记录备注。
// 必须以 cfCommentPrefix 开头 —— 拆除时只有备注能稳定认出"这条是我建的"。
const commentOriginRecord = cfCommentPrefix + " 回源落点"

// OriginDNS 是一个回源的落点在 CF 上的实际状态, 后端判定完毕, 前端直接渲染。
type OriginDNS struct {
	OriginID uint `json:"originId"`
	// Zone 管辖该落点的 CF zone; 为空表示落点不归本账号管 (第三方 CDN 的 CNAME 就是这样), 不判定
	Zone string `json:"zone"`
	// Expect 期望的记录, 形如 "A rn-x.example.com → 1.2.3.4 (橙云)"; 没填源站 IP 时为空
	Expect string `json:"expect"`
	// Actual CF 上的实际记录; 一条都没有时为空
	Actual string      `json:"actual"`
	Level  model.Level `json:"level"`
	// Message 一句话结论。为空表示这个回源压根不需要 CF 里有记录, 前端不必展示
	Message string `json:"message"`
	// CanCreate 程序能不能直接把缺的这条建出来
	CanCreate bool `json:"canCreate"`
}

// InspectOriginDNS 检查全部回源的落点在 CF 上是否已经落地。
// 每份凭据的 zone 列表一轮只拉一次: 回源可能有很多条, 每条都重拉一遍纯属浪费。
func InspectOriginDNS(ctx context.Context) ([]OriginDNS, error) {
	origins, err := ListOrigins()
	if err != nil {
		return nil, err
	}
	loc := newCFZoneLocator()
	out := make([]OriginDNS, 0, len(origins))
	for _, o := range origins {
		out = append(out, inspectOriginDNS(ctx, loc, o))
	}
	return out, nil
}

// inspectOriginDNS 检查单个回源。
// 拉取失败一律转成"无法判定"而不是"没有记录" —— 后者会让人跑去建一条已经存在的记录。
func inspectOriginDNS(ctx context.Context, loc *cfZoneLocator, o model.Origin) OriginDNS {
	// Level 一律给个有效值, 前端按 Message 是否为空决定展不展示, 不必再兜一层空级别
	st := OriginDNS{OriginID: o.ID, Level: model.LevelOK}
	if !hostnameTarget(o.Value) {
		// 落点本身就是 IP (或者没填), 解析直接落到它, CF 里不需要有对应记录
		return st
	}

	z, err := loc.locate(ctx, o.Value)
	if err != nil {
		st.Level = model.LevelWarn
		st.Message = "没能确认这个落点归哪个 CF zone 管: " + err.Error()
		return st
	}
	if z == nil {
		// 现有凭据的可见范围里没有管辖它的 zone —— 第三方 CDN 的 CNAME 就长这样, 不是错
		return st
	}

	st.Zone = z.zone
	if want := expectedOriginRecords(o); len(want) > 0 {
		st.Expect = strings.Join(want, "; ")
	}

	records, err := z.client.ListRecords(ctx, z.zoneID, o.Value, "")
	if err != nil {
		st.Level = model.LevelWarn
		st.Message = "读取 CF 上这条记录失败: " + err.Error()
		return st
	}
	return judgeOriginRecord(st, o, records)
}

// judgeOriginRecord 比对期望与实际。
//
// "存在"不等于"能用": 灰云记录 CF 根本不会把流量转过去, 判定上与没建同级。
// 同名多条是正常的 —— A + AAAA 就是双栈落点, 只有同一类型重复才真的说不清该看哪条。
// 登记的源站 IP 只跟同类型的那条比: 填 IPv4 就只管 A 记录, AAAA 是另一条腿。
func judgeOriginRecord(st OriginDNS, o model.Origin, records []cloudflare.DNSRecord) OriginDNS {
	if len(records) == 0 {
		st.Level = model.LevelError
		if len(originAddresses(o.Address)) == 0 {
			st.Message = fmt.Sprintf("%s 里还没有 %s 这条记录, 而这个回源没填源站 IP, 程序建不出来",
				st.Zone, normalizeName(o.Value))
			return st
		}
		st.CanCreate = true
		st.Message = fmt.Sprintf("%s 里还没有这条记录, 流量到了 CF 也转不出去", st.Zone)
		return st
	}

	lines := make([]string, 0, len(records))
	for _, r := range records {
		lines = append(lines, describeOriginRecord(r))
	}
	st.Actual = strings.Join(lines, "; ")

	if grey := greyRecords(records); len(grey) > 0 {
		st.Level = model.LevelError
		st.Message = fmt.Sprintf("%s 记录是灰云的。CF 要求落点必须是橙云记录, 灰云时回源直接失败",
			strings.Join(grey, " / "))
		return st
	}
	if dup := duplicateTypes(records); dup != "" {
		st.Level = model.LevelWarn
		st.Message = fmt.Sprintf("CF 上有多条 %s 记录, 程序不猜哪条算数, 请自行核对", dup)
		return st
	}

	// 登记了几个源站 IP 就该有几条记录: 双栈的话 A 和 AAAA 各要一条, 缺一条就是缺一条腿
	for _, addr := range originAddresses(o.Address) {
		want := recordTypeFor(addr)
		rec, found := recordOfType(records, want)
		switch {
		case !found:
			st.Level = model.LevelWarn
			st.CanCreate = true
			st.Message = fmt.Sprintf("CF 上没有 %s 记录, 这里登记的源站 IP %s 没有落到实处", want, addr)
			return st
		case !sameName(rec.Content, addr):
			st.Level = model.LevelWarn
			st.Message = fmt.Sprintf("CF 上的 %s 记录指向 %s, 与这里登记的 %s 不一致", want, rec.Content, addr)
			return st
		}
	}

	st.Level = model.LevelOK
	st.Message = "解析已就位"
	if len(records) > 1 {
		st.Message = fmt.Sprintf("解析已就位 (%s 各一条)", strings.Join(recordTypes(records), " + "))
	}
	return st
}

// greyRecords 返回没开代理的那些记录的类型名。
func greyRecords(records []cloudflare.DNSRecord) []string {
	var out []string
	for _, r := range records {
		if !r.Proxied {
			out = append(out, r.Type)
		}
	}
	return out
}

// duplicateTypes 返回第一个出现多次的记录类型, 没有重复时为空。
func duplicateTypes(records []cloudflare.DNSRecord) string {
	seen := map[string]int{}
	for _, r := range records {
		seen[r.Type]++
		if seen[r.Type] > 1 {
			return r.Type
		}
	}
	return ""
}

// recordTypes 按出现顺序列出记录类型, 供"双栈各一条"这类结论直接拼文案。
func recordTypes(records []cloudflare.DNSRecord) []string {
	out := make([]string, 0, len(records))
	for _, r := range records {
		out = append(out, r.Type)
	}
	return out
}

// recordOfType 找出指定类型的那条记录。
func recordOfType(records []cloudflare.DNSRecord, recordType string) (cloudflare.DNSRecord, bool) {
	for _, r := range records {
		if strings.EqualFold(r.Type, recordType) {
			return r, true
		}
	}
	return cloudflare.DNSRecord{}, false
}

// CreateOriginRecord 把某个回源缺的那条落点记录建到 CF 上。
// 只补缺失: 已经存在的记录一律不覆盖 —— 那是线上正在生效的解析, 不该被一个按钮悄悄改掉。
func CreateOriginRecord(ctx context.Context, originID uint) (string, error) {
	var o model.Origin
	if err := database.DB.First(&o, originID).Error; err != nil {
		return "", fmt.Errorf("读取回源 %d 失败: %w", originID, err)
	}
	if !hostnameTarget(o.Value) {
		return "", fmt.Errorf("这个回源的落点是 IP 而不是主机名, CF 上不需要有对应记录")
	}
	if len(originAddresses(o.Address)) == 0 {
		return "", fmt.Errorf("先给这个回源填上源站 IP, 程序才知道这条记录该指向哪")
	}

	z, err := newCFZoneLocator().locate(ctx, o.Value)
	if err != nil {
		return "", err
	}
	if z == nil {
		return "", fmt.Errorf("现有的 CF 凭据里没有管辖 %s 的 zone, 确认 Token 的作用范围", o.Value)
	}

	msg, err := ensureOriginRecord(ctx, z.client, z.zoneID, z.zone, o)
	if err != nil {
		return "", err
	}
	if msg == "" {
		return fmt.Sprintf("%s 里的记录已经齐了, 没有重复创建", z.zone), nil
	}
	return msg + "。CF 那边即刻生效, 回源能不能通还要看源站认不认这个名字", nil
}

// ensureOriginRecord 确保 zone 里有"落点主机名 → 源站 IP"的橙云记录。
// 登记了几个源站 IP 就补几条: IPv4 建 A、IPv6 建 AAAA, 双栈就是各一条, 已经有的那条不动。
//
// 返回空字符串表示记录本来就齐、什么也没做; 已存在但是灰云时报错而不是替用户打开代理 ——
// 那条记录可能另有用途, 改代理状态会直接改变它现有的流量路径。
func ensureOriginRecord(ctx context.Context, cf *cloudflare.Client, zoneID, zoneName string, o model.Origin) (string, error) {
	existing, err := cf.ListRecords(ctx, zoneID, o.Value, "")
	if err != nil {
		return "", err
	}
	if grey := greyRecords(existing); len(grey) > 0 {
		return "", fmt.Errorf("%s 的 %s 记录是灰云的, 回源落点必须是橙云记录, 请先在 CF 里打开代理",
			o.Value, strings.Join(grey, " / "))
	}

	addrs := originAddresses(o.Address)
	if len(addrs) == 0 {
		if len(existing) > 0 {
			return "", nil
		}
		return "", fmt.Errorf("%s 里还没有 %s 这条记录, 请先在回源配置里填上源站 IP, 或自己去 CF 建好这条橙云记录",
			zoneName, o.Value)
	}

	// 同一类型只建一条: 登记了两个同族地址时不往 CF 里堆出一对说不清该看哪条的记录
	covered := map[string]bool{}
	for _, r := range existing {
		covered[strings.ToUpper(r.Type)] = true
	}
	var created []string
	for _, addr := range addrs {
		recordType := recordTypeFor(addr)
		if covered[recordType] {
			continue
		}
		_, err := cf.CreateRecord(ctx, zoneID, cloudflare.NewRecord{
			Type:    recordType,
			Name:    o.Value,
			Content: addr,
			Proxied: true,
			Comment: commentOriginRecord,
		})
		if err != nil {
			return "", err
		}
		covered[recordType] = true
		created = append(created, fmt.Sprintf("%s → %s", recordType, addr))
	}
	if len(created) == 0 {
		return "", nil
	}
	return fmt.Sprintf("已在 %s 给 %s 建好橙云记录 %s", zoneName, o.Value, strings.Join(created, "; ")), nil
}

// expectedOriginRecords 渲染这个回源期望在 CF 上有的那 (几) 条记录, 供界面照着核对。
func expectedOriginRecords(o model.Origin) []string {
	addrs := originAddresses(o.Address)
	out := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		out = append(out, fmt.Sprintf("%s %s → %s (橙云)", recordTypeFor(addr), normalizeName(o.Value), addr))
	}
	return out
}

// originAddresses 把回源上登记的源站 IP 拆成一组。
// 双栈要在同一个落点上同时挂 A 和 AAAA, 所以这一栏允许用逗号 / 空白分隔填多个地址。
func originAddresses(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '，' || r == ';' || r == ' ' || r == '\t' || r == '\n'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// lookupOriginRecord 在指定 zone 里查落点记录的实际状态, 供巡检使用。
// 落点不归这个区管时不查: "没查到"与"不存在"在判定上意义完全相反, 混在一起会误报记录没建。
func lookupOriginRecord(ctx context.Context, cf *cloudflare.Client, zoneID, zoneName, host string) model.OriginRecordState {
	if !inZone(host, zoneName) {
		return model.OriginRecordState{}
	}
	records, err := cf.ListRecords(ctx, zoneID, host, "")
	if err != nil {
		return model.OriginRecordState{}
	}
	state := model.OriginRecordState{Checked: true, Found: len(records) > 0}
	if state.Found {
		state.Type = records[0].Type
		state.Content = records[0].Content
		state.Proxied = records[0].Proxied
	}
	return state
}

// hostnameTarget 判定落点值是不是主机名。是 IP 的落点解析直接落到它, CF 里不需要有对应记录。
func hostnameTarget(value string) bool {
	v := normalizeName(value)
	return strings.Contains(v, ".") && recordTypeFor(v) == "CNAME"
}

// describeOriginRecord 把一条落点记录渲染成给人核对的一行, 代理状态一并带上 —— 灰云是这里最常见的坑。
func describeOriginRecord(r cloudflare.DNSRecord) string {
	proxy := "灰云"
	if r.Proxied {
		proxy = "橙云"
	}
	return fmt.Sprintf("%s %s → %s (%s)", r.Type, r.Name, r.Content, proxy)
}

// cfZone 是一次定位的结果: 用哪份凭据、去哪个区操作。
type cfZone struct {
	client         *cloudflare.Client
	credentialID   uint
	credentialName string
	zoneID         string
	zone           string
}

// cfAccount 是一份 CF 凭据以及它看得见的 zone。
type cfAccount struct {
	credentialID   uint
	credentialName string
	client         *cloudflare.Client
	zones          []cloudflare.Zone
}

// cfZoneLocator 按主机名反查管辖它的 CF zone (以及该用哪份凭据去动它)。
// zone 列表按凭据缓存, 一轮里每份凭据只拉一次。
type cfZoneLocator struct {
	loaded bool
	// loadErr 上一次拉取的结论, 成功也要记 —— 只记 loaded 的话, 第二个回源会拿到
	// "加载过了, 没有错" 的假象, 于是被判成"不归我们管", 正是要防的那种误判
	loadErr  error
	accounts []cfAccount
	// failures 读不到 zone 列表的凭据。有它在就不能断言"这个名字不归我们管",
	// 否则一次网络抖动会让所有落点集体变成"第三方 CDN, 不用管"
	failures []string
}

func newCFZoneLocator() *cfZoneLocator { return &cfZoneLocator{} }

// locate 找出管辖该主机名的 zone。都读到了却没匹配上时返回 (nil, nil), 表示这个名字不在本账号的 CF 里。
func (l *cfZoneLocator) locate(ctx context.Context, host string) (*cfZone, error) {
	if err := l.load(ctx); err != nil {
		return nil, err
	}
	var best *cfZone
	for _, acc := range l.accounts {
		for _, z := range acc.zones {
			name := normalizeName(z.Name)
			if !inZone(host, name) {
				continue
			}
			// 同时存在 example.com 与 sub.example.com 时, 更长的那个才是对它有权威的区
			if best != nil && len(best.zone) >= len(name) {
				continue
			}
			best = &cfZone{
				client:         acc.client,
				credentialID:   acc.credentialID,
				credentialName: acc.credentialName,
				zoneID:         z.ID,
				zone:           name,
			}
		}
	}
	if best == nil && len(l.failures) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(l.failures, "; "))
	}
	return best, nil
}

// zoneOptions 列出所有 CF 凭据可见的 zone 以及它归谁管, 去重后按名字排序。
// 多份凭据看得见同一个 zone 是常见的 (同一个账号发了两个 Token), 表单里不该出现两条一样的选项。
func (l *cfZoneLocator) zoneOptions(ctx context.Context) ([]CFZone, error) {
	if err := l.load(ctx); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := make([]CFZone, 0, 16)
	for _, acc := range l.accounts {
		for _, z := range acc.zones {
			name := normalizeName(z.Name)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, CFZone{Zone: name, CredentialID: acc.credentialID, CredentialName: acc.credentialName})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Zone < out[j].Zone })
	return out, nil
}

// load 拉齐所有 CF 凭据的可见 zone, 结论缓存起来供后续回源复用。
// 单份凭据不可用不阻断其余凭据, 但要记进 failures。
func (l *cfZoneLocator) load(ctx context.Context) error {
	if !l.loaded {
		l.loaded = true
		l.loadErr = l.fetchZones(ctx)
	}
	return l.loadErr
}

func (l *cfZoneLocator) fetchZones(ctx context.Context) error {
	creds, err := CredentialsByKind(model.CredCloudflare)
	if err != nil {
		return fmt.Errorf("读取 CF 凭据失败: %w", err)
	}
	if len(creds) == 0 {
		return fmt.Errorf("还没有配置 Cloudflare 凭据")
	}
	for _, c := range creds {
		client, err := cloudflareClient(c.ID)
		if err != nil {
			l.failures = append(l.failures, fmt.Sprintf("凭据 %s 不可用 (%s)", c.Name, err.Error()))
			continue
		}
		zones, err := client.ListZones(ctx)
		if err != nil {
			l.failures = append(l.failures, fmt.Sprintf("读取凭据 %s 的 zone 列表失败 (%s)", c.Name, err.Error()))
			continue
		}
		l.accounts = append(l.accounts, cfAccount{
			credentialID:   c.ID,
			credentialName: c.Name,
			client:         client,
			zones:          zones,
		})
	}
	if len(l.accounts) == 0 {
		return fmt.Errorf("没有一份 CF 凭据能用: %s", strings.Join(l.failures, "; "))
	}
	return nil
}
