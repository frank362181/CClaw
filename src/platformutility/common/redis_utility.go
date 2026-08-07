package common

import (
	"errors"

	"github.com/redis/go-redis/v9"
)

func IsKeyNotFound(err error) bool {
	return errors.Is(err, redis.Nil)
}
