/*
* 组件接口与容器
 */

package core

import "sync"

// Component the interface for all UI component widgets
type Component interface {
	// Render 根据给定的终端宽度渲染组件
	Render(width int) []string

	// HandleInput 处理键盘输入（组件获得焦点时调用）
	HandleInput(data string)

	// WantsKeyRelease 是否接收按键释放事件
	WantsKeyRelease() bool

	// Invalidate 清除缓存的渲染状态
	Invalidate()
}

// Focusable marker a component that can show a hardware cursor
type Focusable interface {
	Component

	// SetFocused 焦点变更时调用
	SetFocused(bool)

	// Focused 返回当前的焦点状态
	Focused() bool
}

// Container holds multiple child component
type Container struct {
	mu       sync.RWMutex
	children []Component
}

func isFocusable(c Component) bool {
	_, ok := c.(Focusable)
	return ok
}

func NewContainer() *Container {
	return &Container{
		children: make([]Component, 0),
	}
}

func (c *Container) AddChild(child Component) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.children = append(c.children, child)
}

func (c *Container) RemoveChild(child Component) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i, ch := range c.children {
		if ch == child {
			c.children = append(c.children[:i], c.children[i+1:]...)
			return
		}
	}
}

func (c *Container) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.children = nil
}

func (c *Container) Invalidate() {
	c.mu.RLock()
	children := make([]Component, 0, len(c.children))
	copy(children, c.children)
	c.mu.RUnlock()

	for _, child := range children {
		child.Invalidate()
	}
}

// Render 渲染所有的子组件
func (c *Container) Render(width int) []string {
	c.mu.RLock()
	children := make([]Component, 0, len(c.children))
	copy(children, c.children)
	c.mu.RUnlock()

	var lines []string
	for _, child := range children {
		lines = append(lines, child.Render(width)...)
	}

	return lines
}
