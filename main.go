package main

import (
	"fmt"

	_ "project/docs"

	_ "github.com/swaggo/files"
	_ "github.com/swaggo/gin-swagger"

	"my.favorite/platformutility/common"
)

// @title           订单系统 API
// @version         1.0
// @description     商业化订单系统接口文档
// @termsOfService  http://swagger.io/terms/

// @contact.name   API 支持
// @contact.url    http://www.example.com/support
// @contact.email  support@example.com

// @license.name  Apache 2.0
// @license.url   http://www.apache.org/licenses/LICENSE-2.0.html

// @host      localhost:8080
// @BasePath  /api/v1

// @securityDefinitions.apikey ApiKeyAuth
// @in header
// @name Authorization
func main() {
	configFileName := common.GetOSUtilityInstance().GetCurrentDir() + "/cclaw.ini"

	fmt.Println(configFileName)

}
