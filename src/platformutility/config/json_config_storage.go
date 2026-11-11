// JsonConfigStore 是 JSON 配置文件的存储实现（IConfigurationStorage）
// 与 INI 不同，JSON 是层级嵌套结构，没有 "section" 概念
// 因此本实现的 section 参数被忽略，键名统一以 "a.b.c" 的点分路径传入 key 参数，
// 例如 server.host、database.connection.poolSize；Get/Set/Delete 都按点分路径导航
//
// 数据流：JsonConfigParser.Load 只负责读取文件并把原始内容写入 data（字符串，保持 parser
// 原样不变）；本文件内的私有 loadFromContent() 在首次访问时惰性把 data 解析为配置树 tree，
// 之后 Get/Set/Delete 都在 tree 上进行，避免每次访问都重复解析。
//
// 线程安全：所有读操作加 RLock，写操作加 Lock；惰性解析在写锁内完成且只执行一次
// （ensureParsed / ensureParsedLocked 双重判断），多个 goroutine 可安全并发读写

package config

import (
	"encoding/json"
	"strings"
	"sync"

	"my.favorite/platformutility/common"
	"my.favorite/platformutility/exception"
)

type JsonConfigStore struct {
	mutex sync.RWMutex

	data   string                 // 原始 JSON 内容（由 parser 直接写入，作为初始快照）
	tree   map[string]interface{} // 解析后的配置树
	parsed bool                   // tree 是否已从 data 解析
}

func (j *JsonConfigStore) Get(section string, key string) (string, error) {
	return j.GetWithDefault(section, key, "")
}

// GetWithDefault 按点分路径 key 读取配置值，未命中时返回 defaultValue
func (j *JsonConfigStore) GetWithDefault(section string, key string, defaultValue string) (string, error) {
	key = strings.TrimSpace(key)
	if len(key) == 0 {
		return defaultValue, exception.ParameterEmptyErrorEx("key must not be empty")
	}

	if err := j.ensureParsed(); err != nil {
		return defaultValue, err
	}

	j.mutex.RLock()
	defer j.mutex.RUnlock()

	value, ok := j.lookup(common.SplitString(key))
	if !ok {
		return defaultValue, nil
	}

	strValue, ok := common.ValueToString(value)
	if !ok {
		return defaultValue, nil
	}

	return strValue, nil
}

// Set 按点分路径 key 写入配置值，中间节点不存在时自动创建对象
func (j *JsonConfigStore) Set(section string, key string, value string) error {
	key = strings.TrimSpace(key)
	if len(key) == 0 {
		return exception.ParameterEmptyErrorEx("key must not be empty")
	}

	j.mutex.Lock()
	defer j.mutex.Unlock()

	if err := j.ensureParsedLocked(); err != nil {
		return err
	}

	segments := common.SplitString(key)
	if len(segments) == 0 {
		return exception.ParameterEmptyErrorEx("key must not be empty")
	}

	j.setPathLocked(segments, common.ToJson(value))
	return nil
}

// DeleteKey 删除点分路径 key 对应的节点（叶子或子树）
func (j *JsonConfigStore) DeleteKey(section string, key string) {
	key = strings.TrimSpace(key)
	if len(key) == 0 {
		return
	}

	j.mutex.Lock()
	defer j.mutex.Unlock()

	if err := j.ensureParsedLocked(); err != nil {
		return
	}

	segments := common.SplitString(key)
	if len(segments) == 0 {
		return
	}

	// 单段路径：直接删顶层键
	if len(segments) == 1 {
		delete(j.tree, segments[0])
		return
	}

	// 多段路径：定位父对象后再删除最后一个键
	parent, ok := j.lookupPath(segments[:len(segments)-1])
	if !ok {
		return
	}
	parentMap, ok := parent.(map[string]interface{})
	if !ok {
		return
	}
	delete(parentMap, segments[len(segments)-1])
}

// DeleteSection
// /*JSON 没有 section 概念，参数按点分路径解释，删除该路径对应的整棵子树
func (j *JsonConfigStore) DeleteSection(section string) {
	j.DeleteKey("", section)
}

func (j *JsonConfigStore) Destroy() {
	j.mutex.Lock()
	defer j.mutex.Unlock()

	j.tree = nil
}

// ================================== private functions & methods ===================================

// ensureParsed 保证配置树已从 data 解析完成
//
// 本方法**自己管理锁**,调用方不得持有任何锁(RWMutex 的 RLock 不可重入)
// 采用"快路径只读判断 + 慢路径写锁双检":
//   - 已解析时只拿一次 RLock,读操作之间仍可并发
//   - 未解析时升级为写锁,双检后解析,保证只解析一次且不与读并发
func (j *JsonConfigStore) ensureParsed() error {
	j.mutex.RLock()
	parsed := j.parsed
	j.mutex.RUnlock()

	if parsed {
		return nil
	}

	j.mutex.Lock()
	defer j.mutex.Unlock()

	if j.parsed { // 双检:可能已被其他 goroutine 抢先解析
		return nil
	}

	return j.loadFromContent()
}

// ensureParsedLocked 假设调用方已持有写锁，未解析时才执行解析（只解析一次）
func (j *JsonConfigStore) ensureParsedLocked() error {
	if j.parsed {
		return nil
	}
	return j.loadFromContent()
}

// loadFromContent 把 data（原始 JSON 文本）解析进 tree
// 调用方需持有写锁；使用 json.Decoder.UseNumber 保留数字原始精度，避免 float64 精度损失
func (j *JsonConfigStore) loadFromContent() error {
	decoder := json.NewDecoder(strings.NewReader(j.data))
	decoder.UseNumber()

	var root interface{}
	if err := decoder.Decode(&root); err != nil {
		return exception.ConfigErrorEx("invalid json content: " + err.Error())
	}

	// 根节点必须是对象，否则无法按 key 导航
	m, ok := root.(map[string]interface{})
	if !ok {
		return exception.ConfigErrorEx("json root must be a json object")
	}

	j.tree = m
	j.parsed = true
	return nil
}

// lookup 按点分路径在配置树中查找节点，调用方需持有读锁
func (j *JsonConfigStore) lookup(segments []string) (interface{}, bool) {
	return j.lookupPath(segments)
}

// lookupPath 按段逐级导航配置树，返回目标节点。调用方需持有读锁
func (j *JsonConfigStore) lookupPath(segments []string) (interface{}, bool) {
	var current interface{} = j.tree
	for _, seg := range segments {
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil, false
		}
		child, ok := m[seg]
		if !ok {
			return nil, false
		}
		current = child
	}
	return current, true
}

// setPathLocked 在配置树中写入节点，中间层自动创建对象。调用方需持有写锁
func (j *JsonConfigStore) setPathLocked(segments []string, value interface{}) {
	if j.tree == nil {
		j.tree = make(map[string]interface{})
	}

	current := j.tree
	for i, seg := range segments {
		if i == len(segments)-1 {
			current[seg] = value
			return
		}

		child, ok := current[seg]
		if !ok {
			next := make(map[string]interface{})
			current[seg] = next
			current = next
			continue
		}

		// 中间节点已存在但不是对象时，替换为对象继续下沉
		next, ok := child.(map[string]interface{})
		if !ok {
			next = make(map[string]interface{})
			current[seg] = next
		}
		current = next
	}
}
