package heaven

import (
	"context"
	"net"
	"strconv"
	"time"

	"my.favorite/platformutility/common"
	"my.favorite/platformutility/config"
)

// ==================== 创建服务端
var server = common.GetSingleton(func() *Server {
	return &Server{}
}).Instance()

func CreateServer(config *config.ConfigurationHelper) IServer {
	addr, _ := config.GetStringValueDefault("socket", "address", "")
	port, _ := config.GetIntValueDefault("socket", "port", DefaultListenPort)
	maxConnections, _ := config.GetIntValueDefault("socket", "maxConnections", MaxConnection)
	bufferSize, _ := config.GetIntValueDefault("socket", "bufferSize", DefaultBufferSize)
	readTimeout, _ := config.GetIntValueDefault("socket", "read_timeout", ReadTimeout)
	writeTimeout, _ := config.GetIntValueDefault("socket", "write_timeout", WriteTimeout)

	ctx, cancel := context.WithCancel(context.Background())
	server.cancel = cancel
	server.ctx = ctx
	server.address = addr
	server.port = port
	server.address = addr + ":" + strconv.Itoa(port)

	server.maxConnections = int32(maxConnections)
	server.activeConnections = 0
	server.bufferSize = int32(bufferSize)
	server.readTimeout = int16(readTimeout)
	server.writeTimeout = int16(writeTimeout)

	return server
}

// ==================创建表示链接的客户端

func NewConnection(server *Server, conn net.Conn, readTimeout int16, writeTimeout int16, bufferSize int32, fn func()) *Connection {
	return &Connection{
		server: server,
		conn:   conn,

		remoteAddr:   conn.RemoteAddr().String(),
		bufferSize:   bufferSize,
		activated:    true,
		readTimeout:  time.Duration(readTimeout) * time.Second,
		writeTimeout: time.Duration(writeTimeout) * time.Second,

		onClose: fn,
	}
}
