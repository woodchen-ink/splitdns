package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/woodchen-ink/go-web-utils/timex"
	"github.com/woodchen-ink/splitdns/server/database"
	"github.com/woodchen-ink/splitdns/server/model"
	"github.com/woodchen-ink/splitdns/server/pkg/czlconnect"
)

// refreshSkew 是提前量: 只剩这么点有效期就当它已经过期。
// 判早了顶多多刷一次, 判晚了就是拿着刚过期的令牌发请求, 失败还发生在别的调用里。
const refreshSkew = 5 * time.Minute

// refreshRetryGap 是刷新失败后的冷却期。
// 前端在轮询登录状态, 没有这个间隔, 一次网络故障就会变成对授权服务器的持续重试。
const refreshRetryGap = time.Minute

var (
	// tokenMu 串行化刷新。并发刷新会拿同一个 refresh_token 换两次,
	// 服务端一旦轮换 refresh_token, 后到的那次就把自己刷废了
	tokenMu        sync.Mutex
	lastRefreshAt  time.Time
	lastRefreshErr error
)

// AccessToken 返回一个可用的 access_token, 临近过期时先刷新。
//
// refresh_token 也被服务端判死时返回 ErrNeedLogin, 调用方据此要求用户重新授权;
// 其余失败 (网络不通、服务端 5xx) 原样返回, 那只是这一次没刷上。
func AccessToken(ctx context.Context) (string, error) {
	tokenMu.Lock()
	defer tokenMu.Unlock()
	return accessTokenLocked(ctx, false)
}

// RefreshSession 强制刷一次令牌并重新拉用户信息。
// 手动入口: 用户在 CZL Connect 改了昵称 / 分组, 或者想确认这台机器的登录还有效。
func RefreshSession(ctx context.Context) error {
	tokenMu.Lock()
	defer tokenMu.Unlock()

	if _, err := accessTokenLocked(ctx, true); err != nil {
		return err
	}
	return syncUserInfoLocked(ctx)
}

// accessTokenLocked 是取令牌的本体, 调用前必须持有 tokenMu。
// force 为真时无视剩余有效期与冷却期, 直接刷。
func accessTokenLocked(ctx context.Context, force bool) (string, error) {
	if authClient == nil {
		return "", errors.New("登录功能未初始化")
	}
	account, err := CurrentAccount()
	if err != nil {
		return "", err
	}
	if account == nil {
		return "", ErrNeedLogin
	}
	if !force && account.AccessToken != "" && timex.Now().Add(refreshSkew).Before(account.ExpiresAt) {
		return account.AccessToken, nil
	}
	// 空 refresh_token 是上一次 invalid_grant 留下的墓碑, 没必要再去问一遍
	if account.RefreshToken == "" {
		return "", ErrNeedLogin
	}
	if !force && lastRefreshErr != nil && timex.Now().Sub(lastRefreshAt) < refreshRetryGap {
		return "", lastRefreshErr
	}

	tok, err := authClient.Refresh(ctx, account.RefreshToken)
	if err != nil {
		// invalid_grant 是终局: refresh_token 被吊销 / 过期 / 已经用掉, 再刷多少次都一样。
		// 其它错误只是这次没刷上, 本地登录态必须留着, 否则断个网就把用户踢出去了。
		if czlconnect.IsInvalidGrant(err) {
			lastRefreshAt, lastRefreshErr = timex.Now(), nil
			if err := clearTokens(); err != nil {
				return "", err
			}
			return "", ErrNeedLogin
		}
		lastRefreshAt = timex.Now()
		lastRefreshErr = fmt.Errorf("刷新令牌失败: %w", err)
		return "", lastRefreshErr
	}

	lastRefreshAt, lastRefreshErr = timex.Now(), nil
	if err := applyToken(account, tok); err != nil {
		return "", err
	}
	return account.AccessToken, nil
}

// applyToken 把新令牌写回库。
//
// 服务端不轮换时不会回 refresh_token, 这种情况必须继续用手上那个旧的 ——
// 拿空值覆盖等于自己把登录态清了, 而且下一次刷新才会暴露, 现场早就没了。
func applyToken(account *model.Account, tok *czlconnect.Token) error {
	account.AccessToken = tok.AccessToken
	account.ExpiresAt = tok.Expiry(timex.Now())
	if tok.RefreshToken != "" {
		account.RefreshToken = tok.RefreshToken
	}
	if tok.Scope != "" {
		account.Scope = tok.Scope
	}
	err := database.DB.Model(&model.Account{}).Where("id = ?", account.ID).Updates(map[string]any{
		"access_token":  account.AccessToken,
		"refresh_token": account.RefreshToken,
		"expires_at":    account.ExpiresAt,
		"scope":         account.Scope,
	}).Error
	if err != nil {
		return fmt.Errorf("保存刷新后的令牌失败: %w", err)
	}
	return nil
}

// clearTokens 清空令牌但保留账号信息, 让界面还能说清"过期的是谁的登录"。
func clearTokens() error {
	err := database.DB.Model(&model.Account{}).Where("1 = 1").Updates(map[string]any{
		"access_token":  "",
		"refresh_token": "",
	}).Error
	if err != nil {
		return fmt.Errorf("清除失效令牌失败: %w", err)
	}
	return nil
}

func resetRefreshBackoff() {
	tokenMu.Lock()
	lastRefreshAt, lastRefreshErr = time.Time{}, nil
	tokenMu.Unlock()
}

// syncUserInfoLocked 拉一次用户信息并更新本地资料, 调用前必须持有 tokenMu。
//
// 撞上 401 时刷新一次再试: 本地那个过期时间只是服务端给的约定, 令牌完全可能被提前作废,
// 而这种情况和"真的要重新登录"长得一样, 不试一次就分不出来。
func syncUserInfoLocked(ctx context.Context) error {
	token, err := accessTokenLocked(ctx, false)
	if err != nil {
		return err
	}
	info, err := authClient.UserInfo(ctx, token)
	if err != nil && czlconnect.IsUnauthorized(err) {
		token, err = accessTokenLocked(ctx, true)
		if err != nil {
			return err
		}
		info, err = authClient.UserInfo(ctx, token)
	}
	if err != nil {
		return fmt.Errorf("拉取用户信息失败: %w", err)
	}

	err = database.DB.Model(&model.Account{}).Where("1 = 1").Updates(map[string]any{
		"remote_id": info.ID,
		"username":  info.Username,
		"nickname":  info.Nickname,
		"email":     info.Email,
		"avatar":    info.Avatar,
		"groups":    info.Groups,
	}).Error
	if err != nil {
		return fmt.Errorf("更新用户信息失败: %w", err)
	}
	return nil
}
