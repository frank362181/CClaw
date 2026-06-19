package main

import (
	"CClaw/common"
	"fmt"
)

func main() {

	publicIP := common.GetOSUtilityInstance().GetPublicIP()

	fmt.Println(publicIP)

	routerIP := common.GetOSUtilityInstance().GetRouterIP()
	fmt.Println(routerIP)

	dt, _ := common.GetDatetimeUtilityInstance().GetCurrentDatetime()

	fmt.Println(dt.DatetimeStr)
}
