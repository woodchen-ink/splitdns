package czlconnect

import (
	"encoding/json"
	"testing"
)

// UserInfo 只声明用得上的字段, 服务端加字段 / 改字段类型都不该让解析失败。
// 这条不变量破过一次: groups 曾经按逗号串声明, 服务端换成 JSON 数组后整个响应解不动 ——
// 而这一步在换令牌之后, 令牌已经拿到手, 界面上却是授权没完成, 重试多少次都一样。
func TestUserInfoIgnoresUndeclaredFields(t *testing.T) {
	body := `{
		"id": 7,
		"username": "wood",
		"nickname": "Wood",
		"email": "wood@example.com",
		"avatar": "https://example.com/a.png",
		"name": "wood",
		"groups": ["admin", "dev"],
		"upstreams": [{"id": 1, "provider_data": {"k": "v"}}]
	}`

	var info UserInfo
	if err := json.Unmarshal([]byte(body), &info); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if info.ID != 7 || info.Username != "wood" || info.Nickname != "Wood" {
		t.Fatalf("声明过的字段没解出来: %#v", info)
	}
}
