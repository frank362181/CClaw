/*
* file:login_service_test.go
* description:
*	service 包内白盒用例:验证认证链 Refresh 滑动窗口判定(needRefresh)。
*	放置于此而非 service_core:needRefresh 是 LoginService 私有方法,
*	放在 service_core 会引入 service_core→service 的 import cycle。
 */

package service

import (
	"testing"

	"my.favorite/cclawservice/request"
	"my.favorite/platformutility/common"
)

func TestNeedRefreshSlidingWindow(t *testing.T) {
	svc := &LoginService{}
	now := common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp
	// 距上次活跃 < idleTTL/3(30min/3=10min) → 不写
	if svc.needRefresh(&request.LoginSession{LastActiveAt: now - 100}) {
		t.Fatalf("should NOT refresh within sliding window")
	}
	// 距上次活跃 ≥ 10min → 写
	if !svc.needRefresh(&request.LoginSession{LastActiveAt: now - 601}) {
		t.Fatalf("should refresh after sliding window")
	}
	// nil 会话
	if svc.needRefresh(nil) {
		t.Fatalf("nil session must not refresh")
	}
}
