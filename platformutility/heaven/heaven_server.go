package heaven

import (
	"context"
	"net"
	"sync/atomic"
	"syscall"
	"time"

	"my.favorite/platformutility/logging"
)

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
	logging.LogInfo("Server started on %s, max connections: %d", s.address, s.maxConnections)

	go s.onListening() //进入到监听

	return nil
}

func (s *Server) Shutdown() {
	logging.LogInfo("Shutting down heaven server...")

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
		logging.LogInfo("all connection finished gracefully")
		break
	case <-time.After(ShutdownTimeout * time.Second):
		logging.LogWarn("timeout, forcing exit")
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
				logging.LogInfo("server is shutdown")
				return
			default:
				logging.LogError("accept error: %v", err)
				continue
			}
		}

		// 判断连接数量是否超过最大数
		if s.isReachedMaxConnections(conn) {
			logging.LogError("max connections reached")
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
