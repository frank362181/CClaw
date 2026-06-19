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
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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

var (
	osUtilityInstance     *OSUtility
	osUtilityInstanceOnce sync.Once
)

// GetOSUtilityInstance GetOSUtility singleton
func GetOSUtilityInstance() *OSUtility {
	osUtilityInstanceOnce.Do(func() {
		osUtilityInstance = &OSUtility{}
	})

	return osUtilityInstance
}

func (o *OSUtility) GetCores() int {
	return runtime.NumCPU()
}

func (o *OSUtility) GetProcessID() int {
	return os.Getpid()
}

func (o *OSUtility) GetCurrentDir() string {
	tmpDir, err := os.Executable()
	if err != nil {
		panic(err)
	}

	//如果是soft link, then get the physical directory
	realPath, err := filepath.EvalSymlinks(tmpDir)
	if err != nil {
		panic(err)
	}

	currentDir := filepath.Dir(realPath)
	return currentDir
}

func (o *OSUtility) GetExtension(filename string) string {
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

// PathExists 判断文件或目录是否存在
func (o *OSUtility) PathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// DirExists 判断目录是否存在,是目录且存在 return true
func (o *OSUtility) DirExists(path string) bool {
	ret, err := os.Stat(path)
	if err != nil {
		return false //路径不存在
	}

	return ret.IsDir()
}

func (o *OSUtility) CreateDir(path string) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		err := os.MkdirAll(path, 0755) //递归创建
		if err != nil {
			panic(err)
		}
	}
}

// ================================= private functions & methods ======================================
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
