/**
* SSEPresenter 是chat.ISink 在HTTP/SSE协议侧的唯一实现
*
* 职责:
*	- 设置SSE响应头、串行化写入(gin.ResponseWriter 非并发安全);
*	- 空闲心跳,避免代理/浏览器因静默断流
*	- 客户端断开检测:停写 + 主动取消上游(与用户点"中断按钮"同源)
*	- 业务错误 --> SSE 事件的映射(interrupt / notice / error)
* 不负责:
*	业务判断(是否已输出、是否该重试)、历史记账、错误分类(按Kind分支)
 */

package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"my.favorite/cclawservice/apperr"
	"my.favorite/cclawservice/chat"
	cCommon "my.favorite/cclawservice/common"
	myException "my.favorite/cclawservice/exception"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/toolexecutor"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/logging"
	"my.favorite/platformutility/webbase/middleware/basic"
)

// SSEPresenter 一次流式聊天协议的适配层
// 声明周期:一个http请求一个实例,Close 必须在Handler返回前调用
type SSEPresenter struct {
	ginCtx     *gin.Context
	respWriter gin.ResponseWriter
	flusher    http.Flusher

	ctx context.Context // 客户端断开时被取消

	chatTurnID string

	interrupt func(turnID string) // 客户端主动取消

	lock           sync.Mutex
	lastAt         time.Time // 最后一次写出的时刻,milli-second
	lastProgressAt time.Time //最后一次出现内容的时刻(OnDelta更新,OnReasoning不更新)
	timedOut       bool      // 看门狗已主动终止:Fail 不再重复报错
	dead           bool      // 客户端是否已经断开
	streamed       bool      // 是否下发过 **正式正文**(不包含思考/工具事件)

	runningTools map[int]struct{}
	heartStop    chan struct{}
	heartDone    chan struct{}
	closeOnce    sync.Once
}

func NewSSEPresenter(gc *gin.Context, turnID string, interrupt func(turnID string)) (*SSEPresenter, bool) {
	flusher, ok := gc.Writer.(http.Flusher)
	if !ok {
		basic.GetBaseResponser().Failure(gc, http.StatusInternalServerError,
			basic.ConstInternalServerError, basic.ConstInternalServerErrorMsg, nil)
		return nil, false
	}

	gc.Header("Content-Type", "text/event-stream")
	gc.Header("Cache-Control", "no-cache")
	gc.Header("Connection", "keep-alive")
	gc.Header("X-Accel-Buffering", "no") // 关闭反向代理缓冲,保证逐块到达浏览器

	presenter := &SSEPresenter{
		ginCtx:         gc,
		respWriter:     gc.Writer,
		flusher:        flusher,
		ctx:            gc.Request.Context(),
		chatTurnID:     turnID,
		interrupt:      interrupt,
		lastAt:         pCommon.GetDatetimeUtilityInstance().GetNow(),
		lastProgressAt: pCommon.GetDatetimeUtilityInstance().GetNow(),
		runningTools:   make(map[int]struct{}),
		heartStop:      make(chan struct{}),
		heartDone:      make(chan struct{}),
	}
	presenter.startHeartbeat()

	return presenter, true
}

func (p *SSEPresenter) IsStreamed() bool {
	return true
}

func (p *SSEPresenter) OnDelta(delta string) {
	p.lock.Lock()
	p.streamed = true
	p.lastProgressAt = pCommon.GetDatetimeUtilityInstance().GetNow()
	p.lock.Unlock()

	p.emit("message", gin.H{
		"reply": delta,
	})
}

func (p *SSEPresenter) OnReasoning(delta string) {
	p.emit("thinking", gin.H{
		"delta": delta,
	})
}

func (p *SSEPresenter) OnToolIntent(index int, name string) {
	p.lock.Lock()
	p.lastProgressAt = pCommon.GetDatetimeUtilityInstance().GetNow()
	p.lock.Unlock()
}

func (p *SSEPresenter) OnStatus(code, text string) {
	p.lock.Lock()
	p.lastProgressAt = pCommon.GetDatetimeUtilityInstance().GetNow()
	p.lock.Unlock()

	p.emit("status", gin.H{
		"status": code,
		"text":   text,
	})
}

func (p *SSEPresenter) OnTool(evt toolexecutor.ToolEvent) {
	p.lock.Lock()
	p.lastProgressAt = pCommon.GetDatetimeUtilityInstance().GetNow()
	switch evt.Phase {
	case cCommon.ChatToolEventRunning:
		p.runningTools[evt.Seq] = struct{}{}
	case cCommon.ChatToolEventDone, cCommon.ChatToolEventError:
		delete(p.runningTools, evt.Seq)
	}
	p.lock.Unlock()

	p.emit("tool", gin.H{
		"phase":  evt.Phase,
		"seq":    evt.Seq,
		"name":   evt.Name,
		"detail": evt.Detail,
	})
}

func (p *SSEPresenter) OnNotice(code, message string) {
	p.emit("notice", gin.H{
		"code": code,
		"msg":  message,
	})
}

func (p *SSEPresenter) OnJournal(err error) {
	if err != nil {
		logging.LogErrorF("chat controller: journal unavailable for turn %s,error=%s", p.chatTurnID, err.Error())
	}
}

func (p *SSEPresenter) OnFinishReason(reason string) {
}

func (p *SSEPresenter) Done() {
	p.emit("message", "[DONE]")
}

// Close 停止心跳并等待其退出,必须defer 在handler里调用
// 否则handler返回后可能有goroutine持有gin.Context并写响应
func (p *SSEPresenter) Close() {
	p.closeOnce.Do(func() {
		close(p.heartStop)
		<-p.heartDone
	})
}

// Fail 把业务错误翻译成 SSE 事件
// 四种情形:
// - 客户端已经断开 -> 静默结束,不再尝试写
// - 用户主动中断 -> 有正文则正常收尾;无正文则发interrupt事件让UI停止正常收尾
// - 已输出正文 -> 先提示可能不完整,再按需给出可行的错误文案,最后[DONE]
// - 尚未输出任何正文 -> 发error事件,notify前端用醒目样式渲染
func (p *SSEPresenter) Fail(err error) {
	p.lock.Lock()
	dead, streamed, timedOut := p.dead, p.streamed, p.timedOut
	p.lock.Unlock()

	if dead || timedOut || p.ctx.Err() != nil {
		return // 已由看门狗给出明确提示并收尾,避免再叠一条 error
	}

	switch {
	case errors.Is(err, myException.ChatCanceledKind):
		if streamed {
			p.emit("message", "[DONE]")
			return
		}

		p.emit("interrupt", gin.H{
			"interrupt": true,
			"msg":       myException.ChatCanceledKind.Msg,
		})

	case streamed:
		p.emit("notice", gin.H{
			"code": cCommon.ChatNoticeIncomplete,
			"msg":  cCommon.ConstChatNoticeIncompleteMsg,
		})

		// 必须显式提醒用户错误(欠费/密钥无效/内容合规/超时等)：
		// 不得因"已输出内容"而降级 -- 否则用户只看到"可能不完整",即不知道原因也无法处理
		if apperr.Notify(err) {
			p.emit("notice", gin.H{
				"code":  cCommon.ChatNoticeError,
				"msg":   myException.GetChatErrorUserMsg(err),
				"level": cCommon.ChatNoticeLevelError,
			})
		}
		p.emit("message", "[DONE]")
	default:
		p.emit("error", gin.H{
			"msg":    myException.GetChatErrorUserMsg(err),
			"notify": apperr.Notify(err),
		})
	}
}

// ================================ private functions & methods ====================================

// 串行写出一个SSE事件
func (p *SSEPresenter) emit(name string, payload any) {
	p.lock.Lock()
	if p.dead {
		p.lock.Unlock()
		return
	}

	if p.ctx.Err() != nil {
		// 客户端断开:停止一切写入,并主动取消上游(与用户点中断同源)
		// 否则会"写错误被忽略后仍空转转发"直到上游自然结束
		p.dead = true
		p.lock.Unlock()
		p.fireInterrupt()
		return
	}

	p.ginCtx.SSEvent(name, payload)
	p.flusher.Flush()
	p.lastAt = pCommon.GetDatetimeUtilityInstance().GetNow()
	p.lock.Unlock()
}

func (p *SSEPresenter) abortStalled(stalled time.Duration, runningTools int) {
	p.lock.Lock()
	if p.timedOut {
		p.lock.Unlock()
		return
	}

	p.timedOut = true
	p.lock.Unlock()

	detail := "模型长时间只有思考,没有产出"
	if runningTools > 0 {
		detail = fmt.Sprintf("%d 个工具执行长时间无进展", runningTools)
	}

	logging.LogWarnF("sse presenter: turn %s stalled for %s, running_tools=%d (%s), abort",
		p.chatTurnID, stalled, runningTools, detail)

	p.emit("notice", gin.H{
		"code":  cCommon.ChatNoticeThinkingTimeout,
		"msg":   cCommon.ConstChatNoticeThinkingTimeoutMsg,
		"level": cCommon.ChatNoticeLevelError,
	})

	// UI 端靠 data="[DONE]" 触发收尾;不发这一条,UI 会一直停在"流式中"
	p.emit("message", "[DONE]")

	p.fireInterrupt() // 必须锁外调用(内部会回打 turnRegistrator)
}

// 在锁外调用中断回调
// 锁内调用有死锁风险
func (p *SSEPresenter) fireInterrupt() {
	if p.interrupt == nil || p.chatTurnID == pCommon.BlankString {
		return
	}

	p.interrupt(p.chatTurnID)
}

// 空闲心跳:长时间无任何事件时发一行保活
// 避免代理/浏览器因静默而主动断开 SSE
func (p *SSEPresenter) startHeartbeat() {
	interval := time.Duration(hCommon.SSEStreamHeartbeatSecs) * time.Second
	if interval <= 0 {
		close(p.heartDone)
		return
	}

	go func() {
		defer close(p.heartDone)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		first := true //首个tick仅仅做keep alive

		for {
			select {
			case <-p.heartStop:
				return
			case <-p.ctx.Done():
				return
			case now := <-ticker.C:
				p.lock.Lock()
				idle, dead := now.Sub(p.lastAt), p.dead
				lastProgress, timedOut := p.lastProgressAt, p.timedOut
				runningToolLength := len(p.runningTools)
				p.lock.Unlock()

				if dead || timedOut {
					return
				}

				// 看门狗必须放在idle判断之前,否则会被下面的continue跳过
				// writing不走idle:思考不停刷新 lastAt,idle永远 < interval
				if !first {
					stalled := now.Sub(lastProgress)
					if stalled > time.Duration(300)*time.Second || (runningToolLength == 0 && stalled >= time.Duration(hCommon.ChatThinkingLimitTimeout)*time.Second) {
						p.abortStalled(stalled, runningToolLength)
						return
					}

					//if runningToolLength == 0 && stalled > time.Duration(hCommon.ChatThinkingLimitTimeout)*time.Second {
					//	p.abortStalled(stalled, runningToolLength)
					//	return
					//}
				}
				first = false

				if idle < interval {
					continue // 期间有事件往来,无需心跳
				}

				p.emit("heartbeat", gin.H{"ts": pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp})
			}
		}
	}()
}

// compile assert
var _ chat.ISink = (*SSEPresenter)(nil)
