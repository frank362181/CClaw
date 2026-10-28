package chat

import (
	"context"
	"time"

	cCommon "my.favorite/cclawservice/common"
	myException "my.favorite/cclawservice/exception"
	"my.favorite/cclawservice/service/core"
)

type ConcurrencyGate struct{}

func (ConcurrencyGate) Acquire(ctx context.Context, agentRef AgentReference) (release func(), err error) {
	limit := agentRef.MaxConcurrence
	if limit <= 0 {
		limit = cCommon.DefaultAgentConcurrency
	}

	waitCtx, cancel := context.WithTimeout(ctx, time.Duration(cCommon.AgentGateWaitTimeout)*time.Second)
	defer cancel()

	release, err = core.GetAgentGateRegistry().Acquire(waitCtx, agentRef.ID, limit)
	if err != nil {
		if ctx.Err() != nil {
			return nil, myException.ChatCanceledKind.Newf("agent gate canceled")
		}

		return nil, myException.ChatBusyKind.Newf("agent %s gate busy", agentRef.ID)
	}

	return release, nil
}
