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
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/log"
)

const (
	// matchActorPrefix is prepended to each GameActor name.
	matchActorPrefix = "match."
	// sessionActorPrefix is prepended to each PlayerSessionActor name.
	sessionActorPrefix = "session."
	// matchCreateTimeout caps how long the gateway waits for the matchmaker.
	matchCreateTimeout = 5 * time.Second
	// matchResolveDeadline is how long ActorOf retries after a remote SpawnOn.
	matchResolveDeadline = 2 * time.Second
)

// requestMatch asks the cluster matchmaker for a new game and resolves it
// to a usable PID. The PID may refer to a remote node; Tells are routed
// transparently by GoAkt.
func requestMatch(ctx context.Context, system actor.ActorSystem) (*actor.PID, string, error) {
	mm, err := system.ActorOf(ctx, MatchmakerActorName)
	if err != nil {
		return nil, "", fmt.Errorf("locate matchmaker: %w", err)
	}
	reply, err := actor.Ask(ctx, mm, &CreateMatch{}, matchCreateTimeout)
	if err != nil {
		return nil, "", fmt.Errorf("matchmaker.CreateMatch: %w", err)
	}
	created, ok := reply.(*MatchCreated)
	if !ok {
		return nil, "", fmt.Errorf("unexpected matchmaker reply type %T", reply)
	}

	deadline := time.Now().Add(matchResolveDeadline)
	var lookupErr error
	for time.Now().Before(deadline) {
		match, err := system.ActorOf(ctx, created.MatchName)
		if err == nil && match != nil {
			return match, created.MatchName, nil
		}
		lookupErr = err
		time.Sleep(50 * time.Millisecond)
	}
	return nil, "", fmt.Errorf("resolve match %s: %w", created.MatchName, lookupErr)
}

// wsHandler upgrades HTTP requests to WebSocket, spawns a PlayerSessionActor,
// and runs a blocking read loop that forwards input to the session actor.
// On disconnect it unsubscribes the session from the game before shutdown.
func wsHandler(system actor.ActorSystem, logger log.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			logger.Errorf("ws accept: %v", err)
			return
		}

		match, matchName, err := requestMatch(r.Context(), system)
		if err != nil {
			_ = conn.Close(websocket.StatusInternalError, "matchmaker unavailable")
			logger.Errorf("ws %v", err)
			return
		}

		sessionName := sessionActorPrefix + uuid.NewString()
		session := &PlayerSessionActor{match: match, conn: conn}
		sessionPID, err := system.Spawn(r.Context(), sessionName, session, actor.WithLongLived())
		if err != nil {
			_ = match.Shutdown(r.Context())
			_ = conn.Close(websocket.StatusInternalError, "session spawn failed")
			logger.Errorf("session spawn (%s): %v", sessionName, err)
			return
		}
		matchLoc := "local"
		if match.IsRemote() {
			matchLoc = "remote@" + match.Path().HostPort()
		}
		logger.Infof("ws connected: session=%s match=%s (%s)", sessionName, matchName, matchLoc)

		for {
			_, data, err := conn.Read(r.Context())
			if err != nil {
				var ce websocket.CloseError
				if !errors.As(err, &ce) {
					logger.Infof("ws read end: %s: %v", sessionName, err)
				}
				break
			}
			var in PlayerInput
			if err := json.Unmarshal(data, &in); err != nil || in.Type != MessageTypeInput {
				continue
			}
			_ = actor.Tell(r.Context(), sessionPID, &in)
		}

		_ = actor.Tell(context.Background(), match, &Unsubscribe{SessionName: sessionName})
		_ = actor.Tell(context.Background(), sessionPID, &closed{})
		_ = conn.Close(websocket.StatusNormalClosure, "")
		logger.Infof("ws closed: session=%s", sessionName)
	}
}
