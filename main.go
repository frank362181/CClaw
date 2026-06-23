package main

import (
	"fmt"

	"my.favorite/platformutility/common"
)

func main() {
	configFileName := common.GetOSUtilityInstance().GetCurrentDir() + "/cclaw.ini"

	fmt.Println(configFileName)

}
