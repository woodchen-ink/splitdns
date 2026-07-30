package service

import (
	"context"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/woodchen-ink/go-web-utils/timex"
	"github.com/woodchen-ink/splitdns/server/database"
	"github.com/woodchen-ink/splitdns/server/pkg/czlconnect"
)

// 这里守的是一条界面上的不变量: **只要一次授权还没有结论, 登录状态就不能显示成"没在等待也没出错"**。
// 前端把那个组合当成"没什么在进行", 会停掉轮询, 于是回调回来了也没人再看一眼 —— 已经踩过一次,
// 现象是后端日志明明写着"授权回调处理完成", 界面却一直停在登录页。
//
// 全部用不到网络: 走不到换令牌那步的分支都在本地判完。

func setupFlowTest(t *testing.T) context.Context {
	t.Helper()
	timex.MustInit("Asia/Shanghai")
	if err := database.Init(filepath.Join(t.TempDir(), "test.db")); err != nil {
		t.Fatalf("准备测试库失败: %v", err)
	}
	InitAuth(czlconnect.Config{ClientID: "test", RedirectURI: "splitdns://callback", Scope: "read"})
	t.Cleanup(func() {
		clearPending()
		authMu.Lock()
		lastLoginError = ""
		authMu.Unlock()
		_ = database.Close()
	})
	return context.Background()
}

// startLoginState 发起一次授权并取出它的 state。
func startLoginState(t *testing.T) string {
	t.Helper()
	authorizeURL, err := StartLogin()
	if err != nil {
		t.Fatalf("发起授权失败: %v", err)
	}
	parsed, err := url.Parse(authorizeURL)
	if err != nil {
		t.Fatalf("授权地址解析失败: %v", err)
	}
	state := parsed.Query().Get("state")
	if state == "" {
		t.Fatal("授权地址里没有 state")
	}
	return state
}

func loadSession(t *testing.T, ctx context.Context) *Session {
	t.Helper()
	session, err := LoadSession(ctx)
	if err != nil {
		t.Fatalf("读登录状态失败: %v", err)
	}
	return session
}

// 换令牌那一两秒里, 流程仍然要算"进行中"。
func TestSessionStaysWaitingWhileExchanging(t *testing.T) {
	ctx := setupFlowTest(t)
	state := startLoginState(t)

	if s := loadSession(t, ctx); !s.Waiting || s.Exchanging {
		t.Fatalf("刚发起时应当是在等回跳, 实际 waiting=%v exchanging=%v", s.Waiting, s.Exchanging)
	}

	flow, err := beginExchange(state)
	if err != nil {
		t.Fatalf("接手回调失败: %v", err)
	}
	if s := loadSession(t, ctx); !s.Waiting || !s.Exchanging {
		t.Fatalf("换令牌期间必须仍算进行中, 实际 waiting=%v exchanging=%v", s.Waiting, s.Exchanging)
	}

	finishFlow(flow, context.Canceled)
	s := loadSession(t, ctx)
	if s.Waiting || s.Exchanging {
		t.Fatalf("收尾后不该还在进行中, 实际 waiting=%v exchanging=%v", s.Waiting, s.Exchanging)
	}
	if s.LoginError == "" {
		t.Fatal("失败收尾必须留下原因, 否则界面既不等待也不报错")
	}
}

// 同一个授权码回来第二次不能被再换一遍。
func TestSecondCallbackRejectedWhileExchanging(t *testing.T) {
	setupFlowTest(t)
	state := startLoginState(t)

	if _, err := beginExchange(state); err != nil {
		t.Fatalf("第一次接手就失败了: %v", err)
	}
	if _, err := beginExchange(state); err == nil {
		t.Fatal("同一次流程被接手了两次")
	}
}

// 过期回调 (浏览器那个停住的标签页刷新一下就会再发一次) 不能把用户新发起的流程判死。
func TestStaleCallbackKeepsFreshFlow(t *testing.T) {
	ctx := setupFlowTest(t)
	staleState := startLoginState(t)
	freshState := startLoginState(t)
	if staleState == freshState {
		t.Fatal("两次授权的 state 不该相同")
	}

	err := CompleteLogin(ctx, "splitdns://callback?code=whatever&state="+staleState)
	if err == nil {
		t.Fatal("state 对不上的回调应当被拒")
	}
	s := loadSession(t, ctx)
	if !s.Waiting {
		t.Fatal("新发起的那次流程被过期回调清掉了")
	}
	// 新流程还在, 它自己的回调仍然接得住
	if _, err := beginExchange(freshState); err != nil {
		t.Fatalf("新流程无法继续: %v", err)
	}
}

// 应用没开着时点的授权 (冷启动) 换不出令牌, 但必须把原因说清楚。
func TestCallbackWithoutFlowReportsReason(t *testing.T) {
	ctx := setupFlowTest(t)

	err := CompleteLogin(ctx, "splitdns://callback?code=whatever&state=nothing")
	if err == nil {
		t.Fatal("没有正在进行的登录时不该当成成功")
	}
	if s := loadSession(t, ctx); s.LoginError == "" {
		t.Fatal("界面拿不到失败原因, 只会停在登录页干等")
	}
}
