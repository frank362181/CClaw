package main

import (
	"fmt"

	"my.favorite/platformutility/common"
	"my.favorite/platformutility/config"
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
		err = helper.LoadConfiguration()
		if err != nil {
			fmt.Println(err.Error())
			return
		}
	}

	tmpServer, _ := helper.GetStringValue("config", "server")
	fmt.Println(tmpServer)

	tmpFileName := "/Volumes/Wisdom/MyIdea/CClaw/cclaw.ini"
	helper.SaveConfiguration(tmpFileName)
}
