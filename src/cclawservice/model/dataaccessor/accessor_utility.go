package dataaccessor

import (
	"gorm.io/gorm"
	"gorm.io/plugin/optimisticlock"
)

// WriteBackRevivedVersion 复活软删记录后,回读最新 d_version 写回内存结构体。
// 防止仓库缓存携带旧版本(如 0),导致客户端用缓存对象做下一次 Update 时误报 409 版本冲突。
// best-effort:回读失败不影响复活结果(DB 已成功更新),仅保留原版本。
func WriteBackRevivedVersion(g *gorm.DB, model interface{}, idColumn, idValue string, ver *optimisticlock.Version) {
	var v int64
	err := g.Model(model).
		Where(idColumn+" = ? AND deleted = 0", idValue).
		Select("d_version").Scan(&v).Error
	if err != nil {
		return
	}
	ver.Int64 = v
	ver.Valid = true
}
