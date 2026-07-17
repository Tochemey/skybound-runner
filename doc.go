// Package main implements a browser-playable Super Mario platformer
// powered by the GoAkt actor framework.
//
// Each WebSocket connection spawns a local PlayerSessionActor that bridges
// the browser to a GameActor. The GameActor runs a 60 Hz physics tick,
// broadcasts JSON snapshots to subscribers, and shuts down when the session
// disconnects. A cluster-singleton MatchFactory creates one GameActor per
// connection and may place it on any node via SpawnOn.
//
// Wire protocol types and action constants live in types.go. Level layout
// is defined in level.go. HTTP and WebSocket handling is in gateway.go.
package main
