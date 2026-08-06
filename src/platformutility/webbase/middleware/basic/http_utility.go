package basic

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
)

func GetFullUrl(c *gin.Context) string {
	// 1. determine the schema
	schema := "http"
	if c.Request.TLS != nil {
		schema = "https"
	}

	if proto := c.GetHeader("X-Forwarded-Proto"); proto != "" {
		schema = proto
	}

	// 2. determine the hostname
	host := c.Request.Host
	if fwdHost := c.GetHeader("X-Forwarded-Host"); fwdHost != "" {
		host = fwdHost
	}

	// 3. concat them
	uri := c.Request.URL.RequestURI()
	return schema + "://" + host + uri
}

// 命中这些url pattern直接放行
//var allowedUrlPatterns = [...]string{
//	"/heartbeat",
//	"/v2/api-docs.*",
//	"/doc.html",
//	"/index.html",
//	"/manifest.json",
//	"/robots.txt",
//	"/swagger-resources/.*",
//	"/configuration/security",
//	"/service-worker.js",
//	"/favicon.ico",
//	"/swagger-resources",
//	"/precache-manifest.eea302037a9c2783bdf341d6c2dd2ca2.js",
//	"/swagger-ui.html/.*",
//	"/swagger/index.html",
//	"/webjars/.*",
//	"/file/.*", // 文件访问过滤
//}

var allowedUrlPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^/api/heartbeat$`),
	regexp.MustCompile(`^/v2/api-docs(?:/.*)?$`),
	regexp.MustCompile(`^/doc\.html$`),
	regexp.MustCompile(`^/index\.html$`),
	regexp.MustCompile(`^/manifest\.json$`),
	regexp.MustCompile(`^/robots\.txt$`),
	regexp.MustCompile(`^/swagger(?:/.*)?$`), // swagger UI 及静态资源
	regexp.MustCompile(`^/swagger-resources(?:/.*)?$`),
	regexp.MustCompile(`^/configuration/security$`),
	regexp.MustCompile(`^/service-worker\.js$`),
	regexp.MustCompile(`^/favicon\.ico$`),
	regexp.MustCompile(`^/precache-manifest\..*\.js$`),
	regexp.MustCompile(`^/swagger-ui\.html(?:/.*)?$`),
	regexp.MustCompile(`^/webjars/.*$`),
	regexp.MustCompile(`^/file/.*$`),
}

func IsGlobalAllowed(requestUrl string, relativeUrl string, method string) bool {
	u, err := url.Parse(requestUrl)
	if err != nil {
		return false
	}
	// the schema must be invalid
	schema := strings.ToLower(u.Scheme)
	schema = strings.TrimSpace(schema)
	if len(schema) == 0 || len(strings.TrimSpace(u.Host)) == 0 {
		return false
	}

	if (!strings.EqualFold(schema, "http")) && (!strings.EqualFold(schema, "https")) {
		return false
	}

	// OPTIONS (预检请求)
	if strings.EqualFold(method, http.MethodOptions) {
		return true
	}

	// 模式匹配,放行
	for _, pattern := range allowedUrlPatterns {
		if pattern.MatchString(relativeUrl) {
			return true
		}
	}

	return false
}
