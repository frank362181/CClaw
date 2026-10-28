/*
* 线程安全的任意成员的列表
**/

package common

import (
	"sync"

	"my.favorite/platformutility/exception"
)

type SafeList[T any] struct {
	mu sync.RWMutex

	data []T
}

func NewSafeList[T any]() *SafeList[T] {
	return &SafeList[T]{
		data: make([]T, 0),
	}
}

// FindIndex 在 SafeList 中按谓词查找元素下标,未找到返回 -1
func FindIndex[T any](list *SafeList[T], pred func(T) bool) int {
	idx := -1
	list.Range(func(i int, item T) bool {
		if pred(item) {
			idx = i
			return false
		}
		return true
	})
	return idx
}

// Append 在列表末尾添加
func (s *SafeList[T]) Append(item T) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data = append(s.data, item)
}

// AppendEx 如果已经存在则返回表示存在的错误
func (s *SafeList[T]) AppendEx(item T, equal func(a, b T) bool) error {
	if equal == nil {
		return exception.ParameterEmptyErrorEx("comparison function is empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	index, _ := s.find(item, equal)
	if index == -1 {
		s.data = append(s.data, item)
	}

	return exception.MatchErrorEx("target exists")
}

func (s *SafeList[T]) Insert(index int, item T) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if index < 0 || index > len(s.data) {
		return exception.ParameterEmptyErrorEx("index out of range")
	}

	// 扩容并插入
	s.data = append(s.data[:index], append([]T{item}, s.data[index:]...)...)
	return nil
}

func (s *SafeList[T]) InsertEx(index int, item T, equal func(a, b T) bool) error {
	if equal == nil {
		return exception.ParameterEmptyErrorEx("comparison function is empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	idx, _ := s.find(item, equal)
	if idx != -1 {
		return exception.MatchErrorEx("target exists")
	}

	if index < 0 || index > len(s.data) {
		return exception.ParameterEmptyErrorEx("index out of range")
	}

	s.data = append(s.data[:index], append([]T{item}, s.data[index:]...)...)
	return nil
}

func (s *SafeList[T]) Remove(index int) error {
	if index < 0 || index > len(s.data) {
		return exception.ParameterEmptyErrorEx("index out of range")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.data = append(s.data[:index], s.data[index+1:]...)
	return nil
}

func (s *SafeList[T]) Update(index int, item T) error {
	if index < 0 || index > len(s.data) {
		return exception.ParameterEmptyErrorEx("index out of range")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[index] = item
	return nil
}

func (s *SafeList[T]) Get(index int) (T, error) {
	var zero T
	if index < 0 || index > len(s.data) {
		return zero, exception.ParameterEmptyErrorEx("index out of range")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data[index], nil
}

// Range /* 遍历列表，如果f 返回 false,终止遍历
// 遍历期间持有读锁，不需要在f函数重新获得锁，避免死锁 */
func (s *SafeList[T]) Range(fn func(index int, item T) bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for i, v := range s.data {
		if !fn(i, v) {
			break
		}
	}
}

func (s *SafeList[T]) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data = nil
}

func (s *SafeList[T]) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.data)
}

// ============================ private functions & methods =========================
// /* 没有找到就返回-1
func (s *SafeList[T]) find(target T, equal func(a, b T) bool) (int, T) {
	for i, v := range s.data {
		if equal(target, v) {
			return i, v
		}
	}

	var zero T
	return -1, zero
}
