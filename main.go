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

	configFileName := "nodipreg.ini"

	_, err := config.GetConfigurationHelper(configFileName)

	if err != nil {

	}

}
