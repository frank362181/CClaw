package heaven

import (
	"context"
	"net"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"my.favorite/platformutility/logging"

	"my.favorite/platformutility/common"
	"my.favorite/platformutility/config"
)

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

func (s *Server) StartServer() error {
	listenConfig := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var err error
			err = c.Control(func(fd uintptr) {
				err = syscall.SetsockoptInt(syscall.Handle(int(fd)), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
				if err != nil {
					return
				}

				// SO_REUSEPORT = 15,linux
				err = syscall.SetsockoptInt(syscall.Handle(int(fd)), syscall.SOL_SOCKET, 0xf, 1)
			})

			return err
		},
	}

	listener, err := listenConfig.Listen(context.Background(), "tcp", s.address)
	if err != nil {
		return err
	}

	s.listener = listener
	logging.LogInfoF("Server started on %s, max connections: %d", s.address, s.maxConnections)

	go s.onListening() //进入到监听

	return nil
}

func (s *Server) Shutdown() {
	logging.LogInfoF("Shutting down heaven server...")

	s.cancel() //通知所有的链接ctx.Done 得到true

	err := s.listener.Close()
	if err != nil {
		return
	}

	// graceful to exit
	done := make(chan struct{})
	go func() {
		s.wg.Wait() //等待所有的链接退出
		close(done)
	}()

	select {
	case <-done:
		logging.LogInfoF("all connection finished gracefully")
		break
	case <-time.After(ShutdownTimeout * time.Second):
		logging.LogWarnF("timeout, forcing exit")
		break
	default:
		break
	}
}

// =============================== private functions & methods ==================================================
func (s *Server) onListening() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				logging.LogInfoF("server is shutdown")
				return
			default:
				logging.LogErrorF("accept error: %v", err)
				continue
			}
		}

		// 判断连接数量是否超过最大数
		if s.isReachedMaxConnections(conn) {
			logging.LogErrorF("max connections reached")
			continue
		}

		//增加计数
		s.onIncrementConnections()

		//新的链接到来
		s.onNewConnection(s, conn)
	}
}

func (s *Server) isReachedMaxConnections(conn net.Conn) bool {
	if atomic.LoadInt32(&s.activeConnections) >= s.maxConnections {
		_ = conn.Close()
		return true
	}

	return false
}

func (s *Server) onIncrementConnections() {
	atomic.AddInt32(&s.activeConnections, 1)
	s.wg.Add(1)
}

func (s *Server) onDecrementConnections() {
	atomic.AddInt32(&s.activeConnections, -1)
}

func (s *Server) onNewConnection(svr *Server, conn net.Conn) {

}
