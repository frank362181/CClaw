/**
* 本地服务的基础配置信息,来源于INI/JSON等配置文件
**/

package config

import (
	"path/filepath"

	cCommon "my.favorite/cclawservice/common"
	pCommon "my.favorite/platformutility/common"
	pConfig "my.favorite/platformutility/config"
)

type ServiceConfig struct {
	Server struct {
		Port                          string
		SSLEnabled                    bool
		CertDir                       string
		CertFile                      string
		KeyFile                       string
		TrustedProxies                []string
		TrustedForwardedHeaderEnabled bool

		ReadHeaderTimeout int
		ReadTimeout       int

		// writeTimeout=0: 不限制响应写超时。SSE/长任务(工具循环+OCR)的时长上限由应用层
		// chatRunTimeout(默认 AgentRunTimeoutSecs,可用 CCLAW_CHAT_RUN_TIMEOUT_SEC 覆盖)决定,
		// 不再依赖 http.Server 写超时掐断流
		WriteTimeout   int
		IdleTimeout    int
		MaxHeaderBytes int
		SwaggerEnabled bool

		MaxChatConcurrency   int
		MaxStreamConcurrency int
	}

	Security struct {
		BlackUrl []string
		blackIP  []string

		AllowedUrl    []string
		DeSecurityUrl []string

		UnWantedToken []string
		UnTrafficUrl  []string

		AllowedOrigins []string
		SignKey        string
	}
}

func LoadServiceConfig(helper *pConfig.ConfigurationHelper) (ServiceConfig, error) {
	var serverConfig ServiceConfig
	if helper == nil {
		return serverConfig, nil
	}

	// 加载Server的配置信息
	serverConfig.Server.Port, _ = helper.GetStringValue("server", "port")
	serverConfig.Server.SSLEnabled, _ = helper.GetBoolValueDefault("server", "ssl", false)
	serverConfig.Server.ReadTimeout, _ = helper.GetIntValue("server", "readTimeout")
	serverConfig.Server.ReadHeaderTimeout, _ = helper.GetIntValue("server", "readHeaderTimeout")
	serverConfig.Server.WriteTimeout, _ = helper.GetIntValue("server", "writeTimeout")
	serverConfig.Server.IdleTimeout, _ = helper.GetIntValue("server", "idleTimeout")
	serverConfig.Server.MaxHeaderBytes, _ = helper.GetIntValue("server", "maxHeaderBytes")
	serverConfig.Server.CertDir, _ = helper.GetStringValueDefault("server", "certDir", "")
	serverConfig.Server.CertFile, _ = helper.GetStringValue("server", "certFile")
	serverConfig.Server.CertFile = filepath.Join(serverConfig.Server.CertDir, serverConfig.Server.CertFile)
	keyFile, _ := helper.GetStringValue("server", "keyFile")
	serverConfig.Server.KeyFile = filepath.Join(serverConfig.Server.CertDir, keyFile)

	trustedProxies, _ := helper.GetStringValue("server", "trustedProxies")
	serverConfig.Server.TrustedProxies = pCommon.SplitStringEx(trustedProxies, ",")
	serverConfig.Server.TrustedForwardedHeaderEnabled, _ = helper.GetBoolValueDefault("server", "trustedForwardedHeaders", false)

	serverConfig.Server.SwaggerEnabled, _ = helper.GetBoolValueDefault("server", "enableSwagger", false)

	serverConfig.Server.MaxChatConcurrency, _ = helper.GetIntValueDefault("server", "maxChatConcurrency", cCommon.DefaultMaxChatConcurrency)
	serverConfig.Server.MaxStreamConcurrency, _ = helper.GetIntValueDefault("server", "maxStreamConcurrency", cCommon.DefaultMaxStreamConcurrency)

	// 加载security
	serverConfig.Security.SignKey, _ = helper.GetStringValueDefault("security", "signKey", "")
	tempUrl, _ := helper.GetStringValueDefault("security", "allowedUrl", "")
	serverConfig.Security.AllowedUrl = pCommon.SplitStringEx(tempUrl, ",")

	tempUrl, _ = helper.GetStringValueDefault("security", "unTrafficUrl", "")
	serverConfig.Security.UnTrafficUrl = pCommon.SplitStringEx(tempUrl, ",")

	tempUrl, _ = helper.GetStringValueDefault("security", "deSecurityUrl", "")
	serverConfig.Security.DeSecurityUrl = pCommon.SplitStringEx(tempUrl, ",")

	tempUrl, _ = helper.GetStringValueDefault("security", "allowedOrigins", "")
	serverConfig.Security.AllowedOrigins = pCommon.SplitStringEx(tempUrl, ",")

	return serverConfig, nil
}
