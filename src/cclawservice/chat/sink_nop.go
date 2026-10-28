package chat

// NopSink 同步路径空对象
type NopSink struct {
	NopObserver
}

func (NopSink) IsStreamed() bool              { return false }
func (NopSink) OnStatus(code, text string)    {}
func (NopSink) OnTool(event ToolEvent)        {}
func (NopSink) OnNotice(code, message string) {}
func (NopSink) OnJournal(err error)           {}
func (NopSink) OnFinishReason(reason string)  {}
