package main

import (
	"time"

	"my.favorite/tui-engine/tui_engine"
)

func main() {
	logMgr := tui_engine.GetLogManager()
	logMgr.Start()

	// 模拟日志
	go func() {
		levels := []string{"debug", "info", "warn", "error", "fatal"}
		messages := []string{
			"订单服务：新订单创建成功",
			"支付网关：回调处理完成",
			"库存服务：库存低于阈值",
			"数据库连接超时，重试中...",
			"用户 10023 登录 IP 白名单校验",
		}

		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()

		i := 0
		for {
			<-ticker.C
			level := levels[i%len(levels)]
			content := messages[i%len(messages)]

			//print(level, content)
			logMgr.Log(level, content)
			i++
		}
	}()

	//quit := make(chan os.Signal, 1)
	//signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	//
	//<-quit
	//logMgr.GetP().Quit()

	logMgr.Stop()
}
