package m_redis

import (
	"errors"

	"github.com/redis/go-redis/v9"
)

func IsKeyNotFound(err error) bool {
	return errors.Is(err, redis.Nil)
}

func isNetworkError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, redis.ErrClosed) {
		return true
	}

	// 简单检测常见网络错误关键词
	errStr := err.Error()
	if containsAny(errStr) {
		return true
	}

	return false
}

var (
	RedisErrorStrings = []string{
		"connect",
		"connection refused",
		"EOF",
		"broken pipe",
		"i/o timeout",
	}
)

func containsAny(s string) bool {
	sLen := len(s)
	for _, v := range RedisErrorStrings {
		vLen := len(v)
		if sLen >= vLen {
			// 简单包含
			for i := 0; i <= sLen-vLen; i++ {
				if s[i:i+vLen] == v {
					return true
				}
			}
		}
	}

	return false
}
