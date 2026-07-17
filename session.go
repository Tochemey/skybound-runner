package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/coder/websocket"
	"github.com/tochemey/goakt/v4/actor"
)

// writeBudget caps how long a single snapshot write may take before the
// session actor shuts down and applies backpressure on slow clients.
const writeBudget = 50 * time.Millisecond

// PlayerSessionActor bridges one WebSocket connection to a GameActor. It
// subscribes on start, forwards PlayerInput to the game, and writes Snapshot
// JSON frames inline inside Receive.
type PlayerSessionActor struct {
	match *actor.PID
	conn  *websocket.Conn
}

var _ actor.Actor = (*PlayerSessionActor)(nil)

// PreStart is called before the actor begins receiving messages.
func (*PlayerSessionActor) PreStart(*actor.Context) error { return nil }

// PostStop is called after the actor has stopped.
func (*PlayerSessionActor) PostStop(*actor.Context) error { return nil }

// Receive handles subscription, input forwarding, snapshot writes, and shutdown.
func (p *PlayerSessionActor) Receive(ctx *actor.ReceiveContext) {
	switch ctx.Message().(type) {
	case *actor.PostStart:
		ctx.Tell(p.match, &Subscribe{})

	case *PlayerInput:
		ctx.Tell(p.match, ctx.Message())

	case *Snapshot:
		data, err := json.Marshal(ctx.Message())
		if err != nil {
			ctx.Err(err)
			return
		}
		wctx, cancel := context.WithTimeout(ctx.Context(), writeBudget)
		err = p.conn.Write(wctx, websocket.MessageText, data)
		cancel()
		if err != nil {
			ctx.Logger().Infof("ws write failed for %s: %v", ctx.Self().Name(), err)
			ctx.Shutdown()
		}

	case *closed:
		ctx.Shutdown()

	default:
		ctx.Unhandled()
	}
}

// closed is sent by the gateway when the WebSocket connection ends.
type closed struct{}
