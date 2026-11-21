/**
* 产物注册表:计划里只写产物名,执行期解析成真实路径/文本
*
* 原因&目的:
* - 工具产物路径是"上一工具返回的、运行时才知道"值，若让模型自己抄写路径
*	拼错一次就会整轮失败且难以定位
**/

package plan_mode

import (
	"fmt"
	"regexp"
	"sort"
	"sync"

	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/common/hash"
)

var artifactAbsolutePathPattern = regexp.MustCompile(`\$\{([a-zA-Z0-9_#\-]+)\.([a-zA-Z0-9_]+)}`)

// IArtifactMgr 从产物名 -> 产物(parsable)
type IArtifactMgr interface {
	Put(a Artifact)
	Get(name string) (Artifact, bool)
	RenderArg(template string) (string, error)
	RenderArgs(args map[string]string) (map[string]string, error)
}

// ArtifactMgr 单轮计划内的内存实现
type ArtifactMgr struct {
	lock sync.RWMutex

	byName map[string]Artifact
}

func NewArtifactMgr() *ArtifactMgr {
	return &ArtifactMgr{
		byName: make(map[string]Artifact, 8),
	}
}

// Put 注册产物
func (r *ArtifactMgr) Put(a Artifact) {
	if a.Name == pCommon.BlankString {
		return
	}

	if a.Digest == pCommon.BlankString {
		a.Digest, _ = hash.SHA256(a.AbsolutePath)
	}

	r.lock.Lock()
	defer r.lock.Unlock()
	r.byName[a.Name] = a
}

func (r *ArtifactMgr) Get(name string) (Artifact, bool) {
	r.lock.RLock()
	defer r.lock.RUnlock()

	a, ok := r.byName[name]
	return a, ok
}

// RenderArg 渲染模版中的 ${artifcat.field}
func (r *ArtifactMgr) RenderArg(template string) (string, error) {
	var resolveErr error

	out := artifactAbsolutePathPattern.ReplaceAllStringFunc(template, func(s string) string {
		if resolveErr != nil {
			return s
		}

		parts := artifactAbsolutePathPattern.FindStringSubmatch(s)
		name, field := parts[1], parts[2]
		if name == "spec" {
			resolveErr = fmt.Errorf("artifact resolve:${spec.%s} can not appear when runtime", field)
			return s
		}

		artifact, ok := r.Get(name)
		if !ok {
			resolveErr = fmt.Errorf("artifact resolve:artifact %s not found(%s)", name, s)
			return s
		}

		switch field {
		case "text", "path", "ref":
			if artifact.AbsolutePath == pCommon.BlankString {
				resolveErr = fmt.Errorf("artifact resolve:ref %s of artifact %s is nil", field, name)
				return s
			}
			return artifact.AbsolutePath
		default:
			value, hit := artifact.Fields[field]
			if !hit {
				resolveErr = fmt.Errorf("artifact resolve:artifact %s,field %s not found", name, field)
				return s
			}

			return value
		}
	})
	if resolveErr != nil {
		return pCommon.BlankString, resolveErr
	}

	return out, nil
}

func (r *ArtifactMgr) RenderArgs(args map[string]string) (map[string]string, error) {
	if len(args) == 0 {
		return nil, nil
	}

	out := make(map[string]string, len(args))
	for k, v := range args {
		resolved, err := r.RenderArg(v)
		if err != nil {
			return nil, fmt.Errorf("arg %s:%w", k, err)
		}

		out[k] = resolved
	}

	return out, nil
}

// Names 稳定排序的产物名
func (r *ArtifactMgr) Names() []string {
	r.lock.RLock()
	defer r.lock.RUnlock()

	names := make([]string, 0, len(r.byName))
	for name := range r.byName {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
