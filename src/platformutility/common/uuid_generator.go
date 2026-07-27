/*
UUID生成器,全局唯一,外部应用调用 GenerateUUID即可
*/

package common

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
)

var uuidInstance = GetSingleton(func() *uuidGenerator {
	return buildUUIDGenerator()
}).Instance()

type uuidGenerator struct {
	pool sync.Pool
}

func GenerateUUID() string {
	return uuidInstance.generateUUID()
}

// =========================== private functions & methods =====================================
func (g *uuidGenerator) generateUUID() string {
	// 从池中获取数组指针
	buf := g.pool.Get().(*[36]byte)
	defer g.pool.Put(buf)

	// 随机生成 16 字节
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		// 理论上crypto/rand 不会失败,托底
		panic(fmt.Sprintf("Error generating UUID: %v", err))
	}

	//设置 UUID版本 和 变体
	raw[6] = (raw[6] & 0x0f) | 0x40 //version 4
	raw[8] = (raw[8] & 0x3f) | 0x80 //version 8

	// 直接将字节编码到预分配数组,避免任何中间分配
	hex.Encode(buf[:8], raw[:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], raw[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], raw[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], raw[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:], raw[10:])

	// 最后一步转换成string
	return string(buf[:])
}

// BuildUUIDGenerator 构建 UUID 生成器
func buildUUIDGenerator() *uuidGenerator {
	return &uuidGenerator{
		pool: sync.Pool{
			New: func() interface{} {
				// 预分配 36 字节数组 用来存放 UUID
				return &[36]byte{}
			},
		},
	}
}
