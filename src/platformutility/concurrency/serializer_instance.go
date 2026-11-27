/**
* 基于用户、线程的串行化控制
**/

package concurrency

import (
	"strings"
	"sync"

	pCommon "my.favorite/platformutility/common"
)

type SerializerLimiter struct {
	lock   sync.Mutex
	active map[string]struct{}
}

var serializerLimiter = pCommon.GetSingleton(func() *SerializerLimiter {
	return &SerializerLimiter{
		active: make(map[string]struct{}, 64),
	}
}).Instance()

func GetSerializerLimiter() *SerializerLimiter {
	return serializerLimiter
}

func (s *SerializerLimiter) TryEnter(instance string) (func(), bool) {
	instance = strings.TrimSpace(instance)
	if instance == "" {
		return func() {}, true
	}

	s.lock.Lock()
	defer s.lock.Unlock()

	if _, busy := s.active[instance]; busy {
		return nil, false
	}
	s.active[instance] = struct{}{}

	release := func() {
		s.lock.Lock()
		defer s.lock.Unlock()

		delete(s.active, instance)
	}

	return release, true
}
