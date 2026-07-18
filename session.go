/*
MIT License

Copyright (c) 2026 GoAkt Team

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/

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
