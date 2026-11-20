/**
* 协商协议的业务侧扩展点
*
* 职责划分:
* - chat: 协商循环、轮次、与工具选择、消息拼接 -- 只"认识decision + reason + missing + 业务参数"这个形状
* - 业务方(biz/npa):参数 schema、参数解析、判定说明
*
* 接口定义必须在使用方所在的包
**/

package chat

import (
	"encoding/json"
	"strings"

	pCommon "my.favorite/platformutility/common"
)

// INegotiationSpec 协商协议的业务侧实现,三件事:参数长什么样、怎么解析、判定说明写什么
type INegotiationSpec interface {
	// ParamsSchema 终止工具里spec对象字段
	ParamsSchema() map[string]any

	// ParamsRequired 必填字段
	ParamsRequired() []string

	Guard() string // 技能判定说明,写入 system prompt

	DecodeParams(raw json.RawMessage) (any, error)
}

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
