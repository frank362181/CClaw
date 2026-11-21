/**
* 协商协议的业务侧扩展点,通过将不同业务的protocol参数中解析出来
* 不同业务的参数是通过INegotiationSpec实现的
*
* 职责划分:
* - chat: 协商循环、轮次、与工具选择、消息拼接 -- 只"认识decision + reason + missing + 业务参数"这个形状
* - 业务方(biz/npa):参数 schema、参数解析、判定说明
*
* 接口定义必须在使用方所在的包
**/

package plan_mode

import (
	"encoding/json"
	"strings"

	pCommon "my.favorite/platformutility/common"
)

// 获取业务参数
func negotiateParams(spec INegotiationSpec) (map[string]any, []string) {
	if spec == nil {
		return nil, nil
	}

	properties := spec.ParamsSchema()
	if len(properties) == 0 {
		return nil, nil
	}

	required := make([]string, 0, len(spec.ParamsRequired()))
	for _, param := range spec.ParamsRequired() {
		if trimmed := strings.TrimSpace(param); trimmed != "" {
			required = append(required, trimmed)
		}
	}

	return properties, required
}

func negotiateGuard(spec INegotiationSpec) string {
	if spec == nil {
		return pCommon.BlankString
	}

	return spec.Guard()
}

func decodeNegotiationParams(spec INegotiationSpec, raw json.RawMessage) (any, error) {
	if spec == nil || len(raw) == 0 {
		return nil, nil
	}

	return spec.DecodeParams(raw)
}
