/**
* 计划成本估算 -> token / 耗时增量
*
* 设计边界:
* - 只做"协商和计划阶段"的估算,不追求精确;审批页给的是一个可解释的量级;
* - 不依赖SKILL.md的schema,而是依赖于参与的原始材料的"文件数 + 总字节"两个最大的cost 因素
*	跳过artifact与隐藏目录
* - 扫描深度/条数上限,典型案件目录 < 10ms,不会拖慢审批前的预览
*
**/

package cost

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	pCommon "my.favorite/platformutility/common"
)

const (
	// defaultMaterialTokensPerFile 单分材料文件的token估算,800 作为保守值
	defaultMaterialTokensPerFile = 800

	// defaultMaterialTokensCapability 材料 总的上限:避免 500 份材料把预估推到无意义的数字
	defaultMaterialTokensCapability int64 = 80_000

	// defaultMaterialIngestTimeoutPerFile 单份材料解析耗时估算
	defaultMaterialIngestTimeoutPerFile = 200 * time.Millisecond

	// 扫描条数与扫描深度的上限
	defaultMaterialScanLimit = 500
	defaultMaterialScanDepth = 4
)

// PlanPayload  计划规模:只服务预算与canDelegate
type PlanPayload struct {
	TaskCount  int           `json:"taskCount"`
	WaveCount  int           `json:"waveCount"`
	EstTokens  int64         `json:"estTokens"`
	EstLatency time.Duration `json:"estLatency"` // 纳秒

	// 材料规模(审计/复现用):本次估算基于多少分材料、多少字节
	MaterialFiles int   `json:"materialFiles,omitempty"`
	MaterialBytes int64 `json:"materialBytes,omitempty"`
}

// MaterialScaleSnapshot 材料规模快照:计划阶段的成本预估输入
type MaterialScaleSnapshot struct {
	Files     int   `json:"files"`
	Bytes     int64 `json:"bytes"`
	Truncated bool  `json:"truncated,omitempty"`
	Scanned   bool  `json:"scanned,omitempty"`

	MaterialTokensPerFile        int
	MaterialTokensCapacity       int64
	MaterialIngestTimeoutPerFile time.Duration
}

func DefaultScaleSnapShot() MaterialScaleSnapshot {
	return MaterialScaleSnapshot{
		MaterialTokensPerFile:        defaultMaterialTokensPerFile,
		MaterialTokensCapacity:       defaultMaterialTokensCapability,
		MaterialIngestTimeoutPerFile: defaultMaterialIngestTimeoutPerFile,
	}
}

// ScanMaterialScale 统计原始材料目录规模
// - root:目标目录
func ScanMaterialScale(root string) MaterialScaleSnapshot {
	scale := DefaultScaleSnapShot()
	dir := strings.TrimSpace(root)
	if dir == pCommon.BlankString {
		return scale
	}

	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return scale
	}

	scale.Scanned = true
	cleanRoot := filepath.Clean(dir)
	baseDepth := strings.Count(cleanRoot, string(filepath.Separator))
	_ = filepath.WalkDir(cleanRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}

		name := strings.ToLower(entry.Name())
		if entry.IsDir() {
			if path == cleanRoot {
				return nil
			}

			// 生成物 / 依赖 / 缓存:与ingest的跳过集合保持一致
			switch name {
			case "reports", ".git", "node_modules", ".venv", "__pycache__", "temp", "tmp":
				return fs.SkipDir
			}

			if strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}

			if strings.Count(filepath.Clean(path), string(filepath.Separator))-baseDepth >= defaultMaterialScanDepth {
				return fs.SkipDir
			}

			return nil
		}

		if strings.HasPrefix(name, ".") {
			return nil
		}

		fileInfo, statErr := entry.Info()
		if statErr != nil {
			return nil
		}

		scale.Files++
		scale.Bytes += fileInfo.Size()

		if scale.Files >= defaultMaterialScanLimit {
			scale.Truncated = true
			return fs.SkipAll
		}

		return nil
	})

	return scale
}

func EstimateMaterialsToken(scale MaterialScaleSnapshot, perFile int, tokensCap int64) int64 {
	if !scale.Scanned || scale.Files <= 0 {
		return 0
	}

	if perFile <= 0 {
		perFile = defaultMaterialTokensPerFile
	}

	tokens := int64(scale.Files) * int64(perFile)

	if tokensCap <= 0 {
		tokensCap = defaultMaterialTokensCapability
	}

	if tokens > tokensCap {
		tokens = tokensCap
	}

	return tokens
}

func EstimateMaterialLatency(scale MaterialScaleSnapshot, perFileTimeout time.Duration) time.Duration {
	if !scale.Scanned || scale.Files <= 0 {
		return 0
	}

	if perFileTimeout <= 0 {
		perFileTimeout = defaultMaterialIngestTimeoutPerFile
	}

	return time.Duration(float64(scale.Files) * float64(perFileTimeout) * 0.6)
}
