/*
@Summary:
	全局单例容器，T是可以任意类型
@Date:
	18 Jun, 2026
@Author:
	Frank Ng
*/

package common

import "sync"

type Singleton[T any] struct {
	instance *T
	once     sync.Once

	buildFunc func() *T //用来存放工厂函数
}

func GetSingleton[T any](buildFunc func() *T) *Singleton[T] {
	return &Singleton[T]{
		buildFunc: buildFunc,
	}
}

func (s *Singleton[T]) Instance(factory ...func() *T) *T {
	s.once.Do(func() {
		if len(factory) > 0 {
			s.instance = factory[0]()
		} else if s.buildFunc != nil {
			s.instance = s.buildFunc()
		} else {
			panic("singleton no factory provided")
		}
	})

	return s.instance
}
