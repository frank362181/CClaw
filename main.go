package main

import (
	"os"

	"my.favorite/platformutility/common"
	"my.favorite/router/router"
)

func main() {
	configFileName := common.GetOSUtilityInstance().GetCurrentDir() + "/cclaw.ini"
	err := router.StartServer(configFileName)
	if err != nil {
		os.Exit(-1)
	}

	// exit gracefully
	router.ExitGracefully()
}
