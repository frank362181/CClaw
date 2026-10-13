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
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"my.favorite/platformutility/common/hash"
	pException "my.favorite/platformutility/exception"
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

	if rel == ".." || strings.HasPrefix(rel, ".."+sep) || filepath.IsAbs(rel) {
		return "", false
	}

	return rel, true
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

// IsSafeDir 校验 agent 目录是否为安全的相对路径:
// 非空、非绝对路径、无 .. / . 等越界或冗余路径段(该目录会用于 filepath.Join 拼接工作区根)。
func IsSafeDir(dir string) bool {
	dir = strings.TrimSpace(dir)
	if dir == "" || dir == "." || dir == ".." {
		return false
	}
	if filepath.IsAbs(dir) {
		return false
	}

	cleaned := filepath.Clean(dir)
	if cleaned != dir {
		return false // 含冗余分隔符或 . 段(如 a/./b、a//b)
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return false // 仍可能逃逸(如 ../x)
	}

	return true
}

func CreateDir(path string) error {
	return os.MkdirAll(path, 0755) //递归创建
}

func CreateFile(filename string) (*os.File, error) {
	if FileExists(filename) {
		return nil, errors.New("file already exists")
	}

	tempFile, err := os.Create(filename)
	if err != nil {
		return nil, err
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

// ResolvePath 将两个目录合并,并规范化后返回绝对路径
func ResolvePath(input, baseDir string) string {
	if baseDir == "" {
		wd, _ := os.Getwd()
		baseDir = wd
	}

	normalized := NormalizePath(input, "", false, false, false, false)
	normalizedBaseDir := NormalizePath(baseDir, "", false, true, false, false)

	if filepath.IsAbs(normalized) {
		return filepath.Clean(normalized)
	}

	joined := filepath.Join(normalizedBaseDir, normalized)
	if !filepath.IsAbs(joined) {
		joined, _ = filepath.Abs(joined)
	}

	return filepath.Clean(joined)
}

// ResolveRealPath 解析路径的真实路径;文件不存在时沿父目录链向上找最近存在的祖先,
// 解析其真实路径后拼接剩余部分,保证符号链接前缀(如 macOS /var -> /private/var)也被正确解析。
func ResolveRealPath(path string) string {
	_, err := filepath.Abs(path)
	if err != nil {
		return ""
	}

	if r, err := filepath.EvalSymlinks(path); err == nil {
		return r
	}

	parent := filepath.Dir(path)
	if parent == path {
		return path
	}

	return filepath.Join(ResolveRealPath(parent), filepath.Base(path))
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

// SafeJoin 解析目标路径并校验其真实路径必须位于 base 之内,
// 用于防目录穿越与符号链接逃逸。
// 省掉重复的根路径解析 —— 包含性判断与错误文案只有 SafeJoinWithRoot 一份实现。
func SafeJoin(base, target string) (string, error) {
	rootAbs, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}

	return SafeJoinWithRoot(ResolveRealPath(filepath.Clean(rootAbs)), target)
}

// SafeJoinWithRoot 使用**已解析的真实根路径**校验目标路径并返回其绝对路径
// 根解析改由调用方负责 —— 便于"一处解析、多处复用"。
func SafeJoinWithRoot(realRoot, target string) (string, error) {
	root := filepath.Clean(strings.TrimSpace(realRoot))
	if root == BlankString || root == "." {
		// 空的根目录
		return BlankString, errors.New("safe join: empty root")
	}
	if !filepath.IsAbs(root) {
		if abs, err := filepath.Abs(root); err == nil {
			root = abs
		}
	}

	tgt := strings.TrimSpace(target)
	if tgt == BlankString {
		return BlankString, errors.New("safe join: empty target")
	}
	if !filepath.IsAbs(tgt) {
		tgt = filepath.Join(root, tgt)
	}

	targetAbs, err := filepath.Abs(tgt)
	if err != nil {
		return "", err
	}
	targetAbs = filepath.Clean(targetAbs)
	realRootPath := ResolveRealPath(root)
	realTarget := ResolveRealPath(targetAbs)
	if realTarget != realRootPath && !strings.HasPrefix(realTarget, realRootPath+string(filepath.Separator)) {
		return BlankString, errors.New("path escape root: target=" + target + " root=" + root)
	}

	return targetAbs, nil
}

func ComputeStringFingerprint(text string, maxBytes int) string {
	if len(text) > maxBytes {
		text = text[:maxBytes]
	}

	fingerPrint, _ := hash.SHA256(text)
	return fingerPrint
}

func ComputeFileFingerprint(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return ComputeFileFingerprint2(info)
}

// ComputeFileFingerprint2 根据os.FileInfo计算文件指纹,避免两次os.Stat
func ComputeFileFingerprint2(info os.FileInfo) string {
	if info == nil {
		return BlankString
	}

	return fmt.Sprintf("%s:%d:%d", info.Name(), info.Size(), info.ModTime().UnixNano())
}

func ReadFileAll(fileName string) ([]byte, error) {
	data, _, err := ReadFile(fileName, math.MaxInt64)
	return data, err
}

// ReadFile 带上大小上限读取文件，防止超大文件导致内存耗尽
func ReadFile(fileName string, maxSize int64) ([]byte, int64, error) {
	info, err := os.Stat(fileName)
	if err != nil {
		return nil, 0, err
	}
	if info.IsDir() {
		return nil, 0, errors.New("prompt repo: not a file: " + fileName)
	}

	fileSize := info.Size()
	if fileSize > maxSize {
		errString := fmt.Sprintf("prompt repo: document too large(size=%d,max=%d):%s", fileSize, maxSize, fileName)
		return nil, 0, errors.New(errString)
	}

	data, err := os.ReadFile(fileName)
	if err != nil {
		return nil, 0, err
	}

	return data, fileSize, nil
}

// ReadFileRange 读取 [start, end) 区间;end <= 0 或超出文件尾时读到 EOF。
func ReadFileRange(path string, start, end int64) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return BlankString, pException.FilePathErrorEx("open history file error: " + err.Error())
	}
	defer func() { _ = file.Close() }()

	length := end - start
	if length <= 0 {
		return BlankString, nil
	}

	buf := make([]byte, length)
	n, err := file.ReadAt(buf, start)
	if err != nil && err != io.EOF {
		return BlankString, pException.FilePathErrorEx("read history file error: " + err.Error())
	}
	return string(buf[:n]), nil
}

// ReadTextIfExists 读取文本文件；不存在返回空串（不是错误）
func ReadTextIfExists(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return BlankString, nil
		}

		return BlankString, pException.FilePathErrorEx("read memory file error: " + err.Error())
	}

	return string(data), nil
}

// WriteTextAtomically 原子写:先写同目录临时文件,再改名覆盖
func WriteTextAtomically(path, content string) error {
	dir := filepath.Dir(path)
	if err := CreateDir(dir); err != nil {
		return pException.FilePathErrorEx2("create memory file dir error: %s", err.Error())
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return pException.FilePathErrorEx2("write memory file error: %s", err.Error())
	}

	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return pException.FilePathErrorEx2("rename memory file error: %s", err.Error())
	}

	return nil
}

func Close(file *os.File) {
	if file != nil {
		err := file.Close()
		if err != nil {
			return
		}
	}
}

func GetOSEnvironments() []string {
	return os.Environ()
}

func GetEnvString(key string) string {
	key = strings.TrimSpace(key)
	return os.Getenv(key)
}

func GetEnvIntDefault(key string, defaultValue int) int {
	if len(key) == 0 {
		return defaultValue
	}

	envValue := GetEnvString(key)
	envValueInt, err := strconv.Atoi(envValue)
	if err == nil && envValueInt > 0 {
		return envValueInt
	}

	return defaultValue
}

// CreateTimerWithContext 根据context的cancel创建一个timer
func CreateTimerWithContext(idle time.Duration, cancel context.CancelFunc) *time.Timer {
	if idle <= 0 {
		return nil
	}

	return time.AfterFunc(idle, cancel)
}

// ValidateUrl 校验URL,防 SSRF:仅允许 https + 公网地址 + (可选,白名单)
func ValidateUrl(rawUrl string, whiteList []string) bool {
	rawUrl = strings.TrimSpace(rawUrl)
	u, err := url.Parse(rawUrl)
	if err != nil || u.Host == BlankString {
		return false
	}

	if !strings.EqualFold(u.Scheme, "https") {
		return false
	}

	host := strings.ToLower(u.Hostname())
	if len(whiteList) > 0 {
		if !Contains(whiteList, host) {
			return false
		}
	}

	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return false
	}

	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
			ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return false
		}
	}

	return true
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
