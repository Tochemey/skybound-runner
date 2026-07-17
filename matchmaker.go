package main

import (
	"github.com/google/uuid"
	"github.com/tochemey/goakt/v4/actor"
)

// MatchFactory is a cluster-singleton actor that spawns a GameActor for each
// incoming WebSocket connection. It uses SpawnOn with LeastLoad placement so
// games may run on any cluster node; WithRelocationDisabled keeps in-flight
// state pinned to the origin node.
type MatchFactory struct{}

var _ actor.Actor = (*MatchFactory)(nil)

// PreStart is called before the actor begins receiving messages.
func (*MatchFactory) PreStart(*actor.Context) error { return nil }

// PostStop is called after the actor has stopped.
func (*MatchFactory) PostStop(*actor.Context) error { return nil }

// Receive handles PostStart logging and CreateMatch spawn requests.
func (f *MatchFactory) Receive(ctx *actor.ReceiveContext) {
	switch ctx.Message().(type) {
	case *actor.PostStart:
		ctx.Logger().Infof("matchmaker ready on %s", ctx.Self().Path().HostPort())

	case *CreateMatch:
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
