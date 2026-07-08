/*
* 定义监听服务端的通用结构体和接口
 */

package heaven

import (
	"context"
	"net"
	"sync"
	"time"
)

type Server struct {
	listener net.Listener
	protocol int // 0 TCP  1 UDP
	address  string
	port     int //监听的端口

	maxConnections    int32 //最大的连接数
	activeConnections int32 //当前的活跃的链接数量

	bufferSize   int32
	readTimeout  int16
	writeTimeout int16

	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc

	lock sync.Mutex
}

type IServer interface {
	StartServer() error //启动服务
	Shutdown()
}

type Connection struct {
	server *Server
	conn   net.Conn

	remoteAddr string

	bufferSize   int32
	readTimeout  time.Duration
	writeTimeout time.Duration

	activated bool //是否活跃

	onClose func() //客户端关闭后,调用服务端的计数器减 1

	closeOnce sync.Once     //确保onClose仅仅执行一次
	done      chan struct{} //关闭信号
}

type IConnection interface {
	Reset(conn net.Conn, readTimeout time.Duration, writeTimeout time.Duration, bufferSize int32, activated bool, onClose func())
	OnEvent() error
	OnRead() error
	OnWrite() error
	OnError() error
	OnClose()
}
