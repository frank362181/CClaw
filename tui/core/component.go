/*
* 组件接口与容器
 */

package core

// Component the interface for all UI component widgets
type Component interface {
	Render(width int) []string
	HandleInput(data string)
	WantsKeyRelease() bool
	Invalidate()
}

// Focusable marker a component that can show a hardware cursor
type Focusable interface {
	Component
	SetFocused(bool)
	Focused() bool
}

// Container holds multiple child component
type Container struct {
	children []Component
}

const cursorMarker = "\x1b_pi:c\x07"

func isFocusable(c Component) bool {
	_, ok := c.(Focusable)
	return ok
}

func NewContainer() *Container {
	return &Container{}
}

func (c *Container) AddChild(child Component) {
	c.children = append(c.children, child)
}

func (c *Container) RemoveChild(child Component) {
	for i, ch := range c.children {
		if ch == child {
			c.children = append(c.children[:i], c.children[i+1:]...)
			return
		}
	}
}

func (c *Container) Clear() {
	c.children = nil
}

func (c *Container) Invalidate() {
	for _, child := range c.children {
		child.Invalidate()
	}
}

func (c *Container) Render(width int) []string {
	var lines []string
	for _, child := range c.children {
		lines = append(lines, child.Render(width)...)
	}

	return lines
}
