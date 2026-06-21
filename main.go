package main

import (
	"CClaw/common"
	"CClaw/config"
	"fmt"
)

func main() {

	publicIP := common.GetOSUtilityInstance().GetPublicIP()

	fmt.Println(publicIP)

	routerIP := common.GetOSUtilityInstance().GetRouterIP()
	fmt.Println(routerIP)

	dt, _ := common.GetDatetimeUtilityInstance().GetCurrentDatetime()

	fmt.Println(dt.DatetimeStr)

	configFileName := "/Volumes/Wisdom/MyIdea/CClaw/nodipreg.ini"

	helper, err := config.GetConfigurationHelper(configFileName)

	if err == nil {
		helper.LoadConfiguration()
	}

	tmpServer, _ := helper.GetStringValue("config", "server")
	fmt.Println(tmpServer)

	tmpFileName := "/Volumes/Wisdom/MyIdea/CClaw/cclaw.ini"
	helper.SaveConfiguration(tmpFileName)
}
