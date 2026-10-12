package toolexecutor

import (
	"runtime"
	"strings"

	"my.favorite/cclawservice/harness/security"
	pCommon "my.favorite/platformutility/common"
)

type SandBoxMgr struct {
	backends []ISandBox
	active   ISandBox //当前激活
}

var sandboxMgr = pCommon.GetSingleton(func() *SandBoxMgr {
	m := &SandBoxMgr{}
	m.registerDefaultSandbox()
	m.activeSandbox()

	return m
}).Instance()

func GetSandboxMgr() *SandBoxMgr {
	return sandboxMgr
}

func (mgr *SandBoxMgr) SelectSandbox(cc security.CallContext) ISandBox {
	if cc.SandboxConfig != nil && len(cc.SandboxConfig.Backend) > 0 {
		for _, b := range mgr.backends {
			if strings.EqualFold(b.Name(), cc.SandboxConfig.Backend) {
				if b.Available() {
					return b
				}

				// 显式指定不可用
				return nil
			}
		}

		// 指定后端名称不存在
	}

	return mgr.active
}

// Available 判断是否支持沙箱
func (mgr *SandBoxMgr) Available() bool {
	return mgr.active != nil
}

// Active 返回是否有可用的沙箱环境
func (mgr *SandBoxMgr) Active() ISandBox {
	return mgr.active
}

func (mgr *SandBoxMgr) registerDefaultSandbox() {
	switch runtime.GOOS {
	case "linux":
		mgr.backends = append(mgr.backends, nil)
	case "darwin":
		mgr.backends = append(mgr.backends, NewSeatbeltSandbox())
	}
}

func (mgr *SandBoxMgr) activeSandbox() {
	for _, b := range mgr.backends {
		if b.Available() {
			mgr.active = b
			return
		}
	}

	mgr.active = nil
}
