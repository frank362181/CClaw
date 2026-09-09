/*
* 用来对用户在登录时做防爆破登录
**/

package core

import (
	"strings"
	"time"

	"my.favorite/platformutility/logging"
	memoryCommon "my.favorite/platformutility/memory_db/common"
)

// 登录爆破保护：账号维度失败计数 + 锁定
// 关键约束：锁定与计数均只返回“统一凭据”，绝不暴露账号状态（配合枚举修复）
const (
	LoginFailWindow   = 15 * time.Minute //失败计数滑动窗口
	LoginMaxFailures  = 5                //窗口内允许的最大失败次数
	LoginLockDuration = 15 * time.Minute //超限后锁定时长

	loginFailKeyPrefix  = "login_fail_"
	loginLocalKeyPrefix = "login_lock_"
)

type ThrottleService struct {
	memoryDB memoryCommon.IMemoryDB
}

func NewThrottleService(memoryDB memoryCommon.IMemoryDB) *ThrottleService {
	return &ThrottleService{
		memoryDB: memoryDB,
	}
}

// IsLocked 账号当前是否锁定（锁定键存在即锁定）。存储故障按未锁定处理(fail-open)
func (service *ThrottleService) IsLocked(userName string) bool {
	if service.memoryDB == nil {
		return false
	}

	_, err := service.memoryDB.Get(loginLocalKeyPrefix + strings.ToLower(strings.TrimSpace(userName)))

	return err == nil
}

// RecordFailure 记录一次失败：达到阈值则锁定，原子性操作，多实例安全
func (service *ThrottleService) RecordFailure(userName string) {
	if service.memoryDB == nil {
		return
	}

	uName := strings.ToLower(strings.TrimSpace(userName))
	val, err := service.memoryDB.IncrementWithExpiration(loginFailKeyPrefix+uName, 1, LoginFailWindow)
	if err != nil {
		logging.LogWarnF("[ThrottleService] record failure failed for user %s: %s", userName, err.Error())
		return
	}

	if val >= LoginMaxFailures {
		_ = service.memoryDB.Set(loginLocalKeyPrefix+uName, "1", LoginLockDuration)
	}
}

func (service *ThrottleService) Reset(userName string) {
	if service.memoryDB == nil {
		return
	}

	uName := strings.ToLower(strings.TrimSpace(userName))
	_, _ = service.memoryDB.Delete(loginFailKeyPrefix+uName, loginLocalKeyPrefix+uName)
}
