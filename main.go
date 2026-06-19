package main

import (
	"CClaw/common"
	"fmt"
)

func main() {
	dt, _ := common.GetDatetimeUtilityInstance().GetCurrentDatetime()

	fmt.Println(dt.DatetimeStr)

	dt1, _ := common.GetDatetimeUtilityInstance().GetCurrentDatetime()

	distance := dt.Distance(*dt1)

	fmt.Println(distance)

	publicIP := common.GetOSUtilityInstance().GetPublicIP()

	fmt.Println(publicIP)
}
