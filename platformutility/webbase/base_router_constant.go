/*
* 定义常用的常量
 */

package webbase

const (
	FilterOrderRecovery = iota + 1 //最外层的Filter
	FilterOrderLogging             // 日志filter
	FilterOrderReqPre              //预处理
	FilterOrderAddress             //地址处理
	FilterOrderSecurity            //安全处理
)
