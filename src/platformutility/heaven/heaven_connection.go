package heaven

import (
	"net"
	"time"

	"my.favorite/platformutility/logging"
)

func (c *Connection) Reset(conn net.Conn, readTimeout time.Duration, writeTimeout time.Duration, bufferSize int32, activated bool, onClose func()) {
	c.conn = conn
	c.readTimeout = readTimeout
	c.writeTimeout = writeTimeout
	c.bufferSize = bufferSize
	c.activated = activated
	c.onClose = onClose
}

func (c *Connection) OnEvent() error {
	return nil
}

func (c *Connection) OnRead() error {
	return nil
}

func (c *Connection) OnWrite() error {
	return nil
}

func (c *Connection) OnError() error {
	return nil
}

func (c *Connection) OnClose() {
	c.closeOnce.Do(func() {
		_ = c.conn.Close()

		close(c.done) //自己的goroutine退出
		if c.onClose != nil {
			c.onClose()
		}

		logging.LogInfo("Connection closed : %s", c.remoteAddr)
	})
}
