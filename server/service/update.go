package service

import (
	"context"
	"crypto/ed25519"
	"log/slog"
	"sync"
	"time"

	"github.com/woodchen-ink/splitdns/server/pkg/selfupdate"
)

// updateRepo 是发布新版本的仓库, 公开仓库读 Release 不需要令牌。
const updateRepo = "woodchen-ink/splitdns"

const (
	// updateFirstCheckDelay 启动后先等一会儿再查: 别和登录、首屏请求挤在一起
	updateFirstCheckDelay = 10 * time.Second
	// updateCheckInterval GitHub 未认证限流 60 次 / 小时 (按 IP, 同一出口的人共享), 查得勤没有意义
	updateCheckInterval = 6 * time.Hour
)

// UpdateState 是更新流程所处的阶段, 前端按它决定显示什么。
type UpdateState string

const (
	UpdateDisabled    UpdateState = "disabled"    // 本地构建 / 没有公钥, 不参与自动更新
	UpdateIdle        UpdateState = "idle"        // 查过了, 已是最新 (或还没查)
	UpdateChecking    UpdateState = "checking"    // 正在查
	UpdateAvailable   UpdateState = "available"   // 有新版本
	UpdateDownloading UpdateState = "downloading" // 正在下载 + 校验
	UpdateInstalling  UpdateState = "installing"  // 已交给安装逻辑, 程序即将重启
)

// UpdateOptions 由桌面壳在启动时给出。server 不知道自己装在哪、怎么替换自己, 那是外壳的事。
type UpdateOptions struct {
	// CurrentVersion 编译时注入的版本号 (vX.Y.Z), 解析不了即视为本地构建
	CurrentVersion string
	// PublicKey 验证 SHA256SUMS 签名的公钥, 为空则整个更新功能关闭 —— 验不了签就不装
	PublicKey ed25519.PublicKey
	// AssetName 给出本机该用的安装包文件名; 返回空串表示这个平台只能手动下载
	AssetName func(tag string) string
	// DownloadDir 安装包下载到哪
	DownloadDir string
	// Apply 拿到校验通过的安装包后执行安装, 成功时负责让程序退出 / 重启
	Apply func(file string) error
}

// UpdateStatus 是 GET /api/update 的返回。
type UpdateStatus struct {
	State          UpdateState `json:"state"`
	CurrentVersion string      `json:"currentVersion"`
	// DisabledReason 仅 disabled 时有值
	DisabledReason string              `json:"disabledReason,omitempty"`
	Latest         *selfupdate.Release `json:"latest"`
	// CanAutoInstall 为 false 时前端只给"打开下载页"
	CanAutoInstall bool       `json:"canAutoInstall"`
	Received       int64      `json:"received"`
	Total          int64      `json:"total"`
	CheckedAt      *time.Time `json:"checkedAt"`
	// Error 最近一次检查或安装的失败原因, 下一次成功后清掉
	Error string `json:"error"`
}

var updater = struct {
	mu     sync.Mutex
	opts   UpdateOptions
	status UpdateStatus
}{status: UpdateStatus{State: UpdateDisabled, DisabledReason: "更新功能未启用"}}

// StartUpdater 配置更新器并起后台定时检查。只应在启动时调用一次。
func StartUpdater(opts UpdateOptions) {
	updater.mu.Lock()
	updater.opts = opts
	updater.status = UpdateStatus{State: UpdateIdle, CurrentVersion: opts.CurrentVersion}
	switch {
	case !validVersion(opts.CurrentVersion):
		updater.status.State = UpdateDisabled
		updater.status.DisabledReason = "本地构建版本不检查更新"
	case len(opts.PublicKey) == 0:
		updater.status.State = UpdateDisabled
		updater.status.DisabledReason = "这个包没有内置更新签名公钥, 请到 GitHub 手动下载新版本"
	}
	disabled := updater.status.State == UpdateDisabled
	updater.mu.Unlock()

	if disabled {
		slog.Info("自动更新未启用", "version", opts.CurrentVersion)
		return
	}
	go func() {
		time.Sleep(updateFirstCheckDelay)
		for {
			if err := CheckUpdate(context.Background()); err != nil {
				slog.Warn("检查更新失败", "err", err)
			}
			time.Sleep(updateCheckInterval)
		}
	}()
}

func validVersion(v string) bool {
	_, ok := selfupdate.ParseVersion(v)
	return ok
}

// GetUpdateStatus 返回当前状态的副本。
func GetUpdateStatus() UpdateStatus {
	updater.mu.Lock()
	defer updater.mu.Unlock()
	return updater.status
}

// CheckUpdate 查一次最新版本。下载 / 安装进行中时不打断, 直接返回。
func CheckUpdate(ctx context.Context) error {
	updater.mu.Lock()
	switch updater.status.State {
	case UpdateDisabled, UpdateChecking, UpdateDownloading, UpdateInstalling:
		updater.mu.Unlock()
		return nil
	}
	prev := updater.status.State
	updater.status.State = UpdateChecking
	opts := updater.opts
	updater.mu.Unlock()

	rel, err := selfupdate.Latest(ctx, updateRepo)

	updater.mu.Lock()
	defer updater.mu.Unlock()
	now := time.Now()
	updater.status.CheckedAt = &now
	if err != nil {
		// 查不到时保留上一次的结论: 网络抖一下不该让"有新版本"的提示消失
		updater.status.State = prev
		updater.status.Error = err.Error()
		return err
	}
	updater.status.Error = ""
	if !selfupdate.Newer(rel.Tag, opts.CurrentVersion) {
		updater.status.State = UpdateIdle
		updater.status.Latest = nil
		updater.status.CanAutoInstall = false
		return nil
	}
	updater.status.State = UpdateAvailable
	updater.status.Latest = rel
	updater.status.CanAutoInstall = canAutoInstall(opts, rel)
	return nil
}

// canAutoInstall 要求安装包和两份校验文件都在。老版本发布时还没有签名, 那种只能手动下载。
func canAutoInstall(opts UpdateOptions, rel *selfupdate.Release) bool {
	if opts.AssetName == nil || opts.Apply == nil {
		return false
	}
	name := opts.AssetName(rel.Tag)
	return name != "" && rel.Find(name) != nil &&
		rel.Find(selfupdate.ChecksumsAsset) != nil && rel.Find(selfupdate.SignatureAsset) != nil
}
