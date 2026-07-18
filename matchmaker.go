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
	"github.com/google/uuid"
	"github.com/tochemey/goakt/v4/actor"
)

// maxPlayersPerMatch caps how many connections share one co-op GameActor.
const maxPlayersPerMatch = 4

// MatchFactory is a cluster-singleton actor that hands out GameActors for
// incoming WebSocket connections. Connections that arrive while a match still
// has open seats join that match (co-op); otherwise a fresh GameActor is
// spawned with SpawnOn and LeastLoad placement so games may run on any
// cluster node. WithRelocationDisabled keeps in-flight state pinned to the
// origin node.
type MatchFactory struct {
	openMatch string // name of the match currently accepting players
	openSeats int    // seats left in openMatch
}

var _ actor.Actor = (*MatchFactory)(nil)

// PreStart is called before the actor begins receiving messages.
func (*MatchFactory) PreStart(*actor.Context) error { return nil }

// PostStop is called after the actor has stopped.
func (*MatchFactory) PostStop(*actor.Context) error { return nil }

// Receive handles PostStart logging and CreateMatch spawn/join requests.
func (f *MatchFactory) Receive(ctx *actor.ReceiveContext) {
	switch ctx.Message().(type) {
	case *actor.PostStart:
		ctx.Logger().Infof("matchmaker ready on %s", ctx.Self().Path().HostPort())

	case *CreateMatch:
		// Fill the open match first so nearby connections play together.
		if f.openMatch != "" && f.openSeats > 0 {
			if _, err := ctx.ActorSystem().ActorOf(ctx.Context(), f.openMatch); err == nil {
				f.openSeats--
				ctx.Logger().Infof("matchmaker: joined %s (%d seats left)", f.openMatch, f.openSeats)
				ctx.Response(&MatchCreated{MatchName: f.openMatch})
				return
			}
			// The open match died (all players left); forget it.
			f.openMatch = ""
			f.openSeats = 0
		}

		name := matchActorPrefix + uuid.NewString()
		pid, err := ctx.ActorSystem().SpawnOn(ctx.Context(), name, &GameActor{},
			actor.WithLongLived(),
			actor.WithPlacement(actor.LeastLoad),
			actor.WithRelocationDisabled())
		if err != nil {
			ctx.Logger().Errorf("matchmaker: SpawnOn(%s) failed: %v", name, err)
			ctx.Err(err)
			return
		}
		f.openMatch = name
		f.openSeats = maxPlayersPerMatch - 1
		placement := "local"
		if pid != nil && pid.IsRemote() {
			placement = "remote@" + pid.Path().HostPort()
		}
		ctx.Logger().Infof("matchmaker: spawned %s (%s)", name, placement)
		ctx.Response(&MatchCreated{MatchName: name})

	default:
		ctx.Unhandled()
	}
}
