/**
* 等带审批的容器类,维护一张等待审批的列表
*
* 采用sender + turnID作为主键,但定位不同：
* - TurnRegistry 是为取消一个正在跑的轮次
* - 本类是一个拒绝/放行一个已就绪、尚未执行的计划
*
* 等待审批对象的生命周期:AwaitApproval 注册 -> 前端 POST/chat/approve -> 唤醒 -> 注销
**/

package security

import (
	"sync"

	"my.favorite/cclawservice/common"
	pCommon "my.favorite/platformutility/common"
)

type ApprovalRegistry struct {
	lock sync.Mutex

	waiters map[string]chan bool
}

var approvalRegistry = pCommon.GetSingleton(func() *ApprovalRegistry {
	return &ApprovalRegistry{
		waiters: make(map[string]chan bool, 8),
	}
}).Instance()

func GetApprovalRegistry() *ApprovalRegistry {
	return approvalRegistry
}

func (r *ApprovalRegistry) Register(sender, turnId string) (<-chan bool, func()) {
	decision := make(chan bool, 1)
	key := sender + "|" + common.NormalizeCompactId(turnId)

	r.lock.Lock()
	r.waiters[key] = decision
	r.lock.Unlock()

	// 返回一个因为超时清理资源的函数
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			r.lock.Lock()
			defer r.lock.Unlock()

			// [必须判等] 同一个 key 可能已被后续 Register 覆盖,
			// 无条件删除会把"新的等待者"删掉, 让新请求永远等不到决策
			if current, ok := r.waiters[key]; ok && current == decision {
				delete(r.waiters, key)
			}
		})
	}

	return decision, cleanup
}

// Decide 前端提交审批结论; false = 没有正在等待的轮次(超时 / 已结束 / 已被中断)
func (r *ApprovalRegistry) Decide(sender, turnId string, approval bool) bool {
	key := sender + "|" + common.NormalizeCompactId(turnId)

	r.lock.Lock()
	decision, ok := r.waiters[key] // 一次决策仅生效一次
	if ok {
		delete(r.waiters, key)
	}
	r.lock.Unlock()

	if !ok {
		return false
	}

	select {
	case decision <- approval:
	default: // 已有结论,幂等
	}

	return true
}

func (r *ApprovalRegistry) Cancel(sender, turnId string) {
	key := sender + "|" + common.NormalizeCompactId(turnId)

	r.lock.Lock()
	decision, ok := r.waiters[key]
	if ok {
		delete(r.waiters, key)
	}
	r.lock.Unlock()

	if !ok {
		return
	}

	select {
	case decision <- false:
	default:
	}
}
