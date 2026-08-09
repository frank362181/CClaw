/*******************************************************
* @Summary:
*	与操作系统相关的工具类
* @Author:
*	Frank Ng
* @Date:
* 	Jun 19, 2026
*******************************************************/

package common

import (
	"bytes"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"my.favorite/platformutility/exception"
)

type hostInfo struct {
	HostName string //本机名称
	HostIPV4 string //本机IP v4
	HostIPV6 string
	RouterIP string //本机出站地址
	PublicIP string
}

type OSUtility struct {
	hostInfo        *hostInfo
	getHostInfoOnce sync.Once

	getHostInfoErr error // optional, to store the error information
}

var osUtilityInstance = GetSingleton(func() *OSUtility {
	return buildOSUtilityInstance()
}).Instance()

// GetOSUtilityInstance GetOSUtility singleton
func GetOSUtilityInstance() *OSUtility {
	return osUtilityInstance
}

func IsWindows() bool {
	osType := getOSType()
	return strings.ToLower(osType) == "windows"
}

func GetCores() int {
	return runtime.NumCPU()
}

func GetProcessID() int {
	return os.Getpid()
}

func IsAbsolutePath(path string) bool {
	return filepath.IsAbs(path)
}

// GetCurrentDir 返回的是工作目录
func GetCurrentDir() string {
	tmpDir, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	//如果是soft link, then get the physical directory
	realPath, err := filepath.EvalSymlinks(tmpDir)
	if err != nil {
		panic(err)
	}

	currentDir := realPath
	return currentDir
}

// GetExecutableDir 返回可执行文件所在目录（解析符号链接后的真实目录）。
// 部署场景下配置文件放置在可执行文件附近，据此定位最可靠，且与启动时的工作目录无关。
func GetExecutableDir() string {
	exePath, err := os.Executable()
	if err != nil {
		panic(err)
	}

	realPath, err := filepath.EvalSymlinks(exePath)
	if err != nil {
		realPath = exePath
	}

	return filepath.Dir(realPath)
}

func GetRelativePath(path, cwd string) (string, bool) {
	resolvedCwd := ResolvePath(cwd, "")
	resolvedPath := ResolvePath(path, resolvedCwd)

	rel, err := filepath.Rel(resolvedCwd, resolvedPath)
	if err != nil {
		return "", false
	}

	sep := string(filepath.Separator)
	if rel == "." || rel == "" {
		return ".", true
	}

	if rel == ".." || strings.HasPrefix(rel, ".."+sep) || IsAbsolutePath(rel) {
		return "", false
	}

	return rel, true
}

func FormatPath(path string) string {
	if len(path) == 0 {
		return ""
	}

	s := strings.ReplaceAll(path, "\\", "/")
	return s
}

func GetExtension(filename string) string {
	if len(strings.TrimSpace(filename)) == 0 {
		return ""
	}

	filename = strings.TrimSpace(filename)
	index := strings.LastIndex(filename, ".")
	if index == -1 || index == len(filename)-1 {
		return ""
	}

	return strings.ToLower(filename[index+1:]) //全部转换成小写
}

// PathExists 判断文件或目录是否存在
func PathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// DirExists 判断目录是否存在,是目录且存在 return true
func DirExists(path string) bool {
	ret, err := os.Stat(path)
	if err != nil {
		return false //路径不存在
	}

	return ret.IsDir()
}

func FileExists(filename string) bool {
	_, err := os.Stat(filename)
	if err == nil {
		return true //文件或目录存在
	}

	if errors.Is(err, os.ErrNotExist) {
		return false //确实不存在
	}

	/*其他错误(如权限不足),通常被认为不能确定是否存在
	* 需要按照 false 返回
	 */
	return false
}

func IsDir(path string) bool {
	ret, err := os.Stat(path)
	if err != nil {
		return false
	}

	return ret.IsDir()
}

func CreateDir(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		err := os.MkdirAll(path, 0755) //递归创建
		if err != nil {
			return err
		}
	}

	return nil
}

func CreateFile(filename string) (*os.File, error) {
	if FileExists(filename) {
		return nil, exception.FilePathErrorEx("file already exists")
	}

	tempFile, err := os.Create(filename)
	if err != nil {
		return nil, exception.FilePathErrorEx(err.Error())
	}

	return tempFile, nil
}

// NormalizePath normalize a path string
var unicodeSpacesRegex = regexp.MustCompile("[\u00A0\u2000-\u200A\u202F\u205F\u3000]")

func NormalizePath(path string, homeDir string, trim bool, expandTilde bool, stripAtPrefix bool, normalizeUnicodeSpace bool) string {
	if len(path) == 0 {
		return ""
	}

	normalizedPath := path
	if trim {
		normalizedPath = strings.TrimSpace(normalizedPath)
	}

	if normalizeUnicodeSpace {
		normalizedPath = unicodeSpacesRegex.ReplaceAllString(normalizedPath, " ")
	}

	if stripAtPrefix && strings.HasPrefix(normalizedPath, "@") {
		normalizedPath = normalizedPath[1:]
	}

	if expandTilde {
		home := homeDir
		if home == "" {
			var err error
			home, err = os.UserHomeDir()
			if err != nil {
				home = ""
			}
		}

		if home != "" {
			if normalizedPath == "~" {
				return home
			}

			if strings.HasPrefix(normalizedPath, "~/") || (IsWindows() && strings.HasPrefix(normalizedPath, "~\\")) {
				return filepath.Join(home, normalizedPath[2:])
			}
		}
	}

	if strings.HasPrefix(normalizedPath, "file://") {
		return FileURLToPath(normalizedPath)
	}

	return normalizedPath
}

func IsLocalPath(path string) bool {
	trimmedPath := strings.TrimSpace(path)
	if len(trimmedPath) == 0 {
		return false
	}

	if strings.HasPrefix(trimmedPath, "npm:") {
		return false
	}
	if strings.HasPrefix(trimmedPath, "git:") {
		return false
	}
	if strings.HasPrefix(trimmedPath, "github:") {
		return false
	}
	if strings.HasPrefix(trimmedPath, "http:") {
		return false
	}
	if strings.HasPrefix(trimmedPath, "https:") {
		return false
	}
	if strings.HasPrefix(trimmedPath, "ssh:") {
		return false
	}

	return true
}

func ResolveFile(baseDir, name string) (string, error) {
	if !IsDir(baseDir) {
		return "", exception.FilePathErrorEx("base dir should be a directory: " + baseDir)
	}

	if IsDir(name) {
		return "", exception.FilePathErrorEx("name could not be a directory: " + name)
	}

	targetFileName := name
	if !IsAbsolutePath(name) {
		targetFileName = filepath.Join(baseDir, targetFileName)
	}

	info, err := os.Stat(targetFileName)
	if err != nil {
		return "", exception.FilePathErrorEx(err.Error())
	}

	if info.IsDir() {
		return "", exception.FilePathErrorEx("target must be a file: " + targetFileName)
	}

	// cleanup . & ..
	return filepath.Clean(targetFileName), nil
}

func ResolvePath(input, baseDir string) string {
	if baseDir == "" {
		wd, _ := os.Getwd()
		baseDir = wd
	}

	normalized := NormalizePath(input, "", false, false, false, false)
	normalizedBaseDir := NormalizePath("", baseDir, false, true, false, false)

	if IsAbsolutePath(normalized) {
		return filepath.Clean(normalized)
	}

	joined := filepath.Join(normalizedBaseDir, normalized)
	if !IsAbsolutePath(joined) {
		joined, _ = filepath.Abs(joined)
	}

	return filepath.Clean(joined)
}

// FileURLToPath converts a file ://URL to an O-native path
func FileURLToPath(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "file" {
		trimmed := strings.TrimPrefix(rawURL, "file://")
		return filepath.FromSlash(trimmed)
	}

	p := u.Path
	if IsWindows() && len(p) > 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}

	return filepath.FromSlash(p)
}

func MarkPathIgnoreByCloudSync(path string) {
	var attrs []string

	switch runtime.GOOS {
	case "darwin":
		attrs = []string{"com.dropbox.ignored", "com.apple.fileprovider.ignore#P"}
	case "linux":
		attrs = []string{"user.com.dropbox.ignored"}
	default:
		return
	}

	for _, attr := range attrs {
		var cmd *exec.Cmd
		if runtime.GOOS == "darwin" {
			cmd = exec.Command("xattr", "-w", attr, "1", path)
		} else {
			cmd = exec.Command("setfattr", "-n", attr, "-v", "1", path)
		}

		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		_ = cmd.Run()
	}
}

func Close(file *os.File) {
	if file != nil {
		err := file.Close()
		if err != nil {
			return
		}
	}
}

func GetEnvString(key string) string {
	return os.Getenv(key)
}

// =================================== member of object ======================

func (o *OSUtility) GetHostName() string {
	o.initHostInfo()

	if o.hostInfo != nil {
		return o.hostInfo.HostName
	}

	return ""
}

func (o *OSUtility) GetHostIP4() string {
	o.initHostInfo()

	if o.hostInfo != nil {
		return o.hostInfo.HostIPV4
	}

	return ""
}

func (o *OSUtility) GetHostIPV6() string {
	o.initHostInfo()
	if o.hostInfo != nil {
		return o.hostInfo.HostIPV6
	}

	return ""
}

func (o *OSUtility) GetRouterIP() string {
	o.initHostInfo()
	if o.hostInfo != nil {
		return o.hostInfo.RouterIP
	}

	return ""
}

func (o *OSUtility) GetPublicIP() string {
	o.initHostInfo()
	if o.hostInfo != nil {
		return o.hostInfo.PublicIP
	}
	return ""
}

// ================================= private functions & methods ======================================
func buildOSUtilityInstance() *OSUtility {
	return &OSUtility{}
}

func getOSType() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "darwin":
		return "MacOS"
	case "linux":
		return "Linux"
	case "freebsd":
		return "freebsd"
	case "openbsd":
		return "openbsd"
	case "plan9":
		return "plan9"
	case "solaris":
		return "solaris"
	default:
		return "unknown"
	}
}

func (o *OSUtility) initHostInfo() {
	o.getHostInfoOnce.Do(func() {
		o.hostInfo, o.getHostInfoErr = o.getHostInfo()
	})
}

func (o *OSUtility) getHostInfo() (*hostInfo, error) {
	hostInfo := &hostInfo{}

	hostName, err := os.Hostname()
	if err != nil {
		return nil, err
	}
	hostInfo.HostName = hostName

	// == get local ip
	adds, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	valid := false
	for _, addr := range adds {
		if aspnet, ok := addr.(*net.IPNet); ok && !aspnet.IP.IsLoopback() {
			if aspnet.IP.To4() != nil {
				hostInfo.HostIPV4 = aspnet.IP.String()
				valid = true
			}

			if valid {
				break
			}
		}

		continue
	}

	// ==== get router ip
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		panic(err)
	}
	defer func(conn net.Conn) {
		err := conn.Close()
		if err != nil {

		}
	}(conn)
	routerAddr := conn.LocalAddr().(*net.UDPAddr)
	hostInfo.RouterIP = routerAddr.IP.String()

	// get public IP
	req, _ := http.NewRequest("GET", "https://icanhazip.com", nil)
	req.Header.Set("User-Agent", "curl/7.64.1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {

		}
	}(resp.Body)

	body, _ := io.ReadAll(resp.Body)
	body = bytes.TrimPrefix(body, []byte{0xEE, 0xBB, 0xBF})
	hostInfo.PublicIP = strings.TrimSpace(string(body))

	return hostInfo, nil
}
