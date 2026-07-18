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
	"embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/discovery/static"
	gerrors "github.com/tochemey/goakt/v4/errors"
	"github.com/tochemey/goakt/v4/log"
	"github.com/tochemey/goakt/v4/remote"
)

const (
	// systemName is the GoAkt actor system identifier.
	systemName = "skybound-runner"
	// readTimeout is the HTTP server read deadline for browser connections.
	readTimeout = 10 * time.Second
	// defaultBindHost is the default host for cluster gossip and remoting.
	defaultBindHost = "127.0.0.1"
)

// webFS embeds the browser client served at the HTTP root.
//
//go:embed web/index.html web/main.js
var webFS embed.FS

var (
	httpPort      = flag.Int("http-port", 8080, "HTTP/WebSocket port for browser clients")
	bindHost      = flag.String("bind-host", defaultBindHost, "Host this node advertises for cluster traffic")
	remotingPort  = flag.Int("remoting-port", 9000, "Port for inter-node actor messaging")
	discoveryPort = flag.Int("discovery-port", 9001, "Gossip port used by the static discovery provider")
	peersPort     = flag.Int("peers-port", 9002, "Cluster peer state-sync port")
	peers         = flag.String("peers", "", "Comma-separated host:discoveryPort list of cluster bootstrap peers")
)

// main boots the GoAkt actor system, spawns the matchmaker singleton,
// and serves the embedded web client plus WebSocket game endpoint.
func main() {
	flag.Parse()
	ctx := context.Background()
	logger := log.DefaultLogger

	system, err := buildActorSystem(logger)
	if err != nil {
		logger.Fatal(err)
	}

	if err := system.Start(ctx); err != nil {
		logger.Fatal(err)
	}

	if _, err := system.SpawnSingleton(ctx, MatchmakerActorName, &MatchFactory{}); err != nil {
		if !errors.Is(err, gerrors.ErrSingletonAlreadyExists) {
			logger.Fatal(err)
		}
		logger.Infof("matchmaker singleton already running elsewhere in the cluster")
	}

	web, err := fs.Sub(webFS, "web")
	if err != nil {
		logger.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", wsHandler(system, logger))
	mux.Handle("/", http.FileServer(http.FS(web)))

	addr := fmt.Sprintf(":%d", *httpPort)
	srv := &http.Server{
		Addr:        addr,
		Handler:     mux,
		ReadTimeout: readTimeout,
	}

	go func() {
		logger.Infof("Skybound Runner listening on http://localhost%s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal(err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	logger.Info("shutting down")

	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutCtx)
	_ = system.Stop(shutCtx)
}

// buildActorSystem creates a cluster-aware ActorSystem with CBOR serializers
// for cross-node message types and static peer discovery.
func buildActorSystem(logger log.Logger) (actor.ActorSystem, error) {
	cbor := remote.NewCBORSerializer()
	remoteCfg := remote.NewConfig(*bindHost, *remotingPort,
		remote.WithSerializers((*PlayerInput)(nil), cbor),
		remote.WithSerializers((*Subscribe)(nil), cbor),
		remote.WithSerializers((*Snapshot)(nil), cbor),
		remote.WithSerializers((*CreateMatch)(nil), cbor),
		remote.WithSerializers((*MatchCreated)(nil), cbor),
		remote.WithSerializers((*Unsubscribe)(nil), cbor),
	)

	discoConfig := &static.Config{Hosts: peerList(*peers, *bindHost, *discoveryPort)}
	disco := static.NewDiscovery(discoConfig)

	clusterCfg := actor.NewClusterConfig().
		WithDiscovery(disco).
		WithDiscoveryPort(*discoveryPort).
		WithPeersPort(*peersPort).
		WithPartitionCount(7).
		WithBootstrapTimeout(10*time.Second).
		WithReadTimeout(3*time.Second).
		WithWriteTimeout(3*time.Second).
		WithKinds(new(GameActor), new(MatchFactory))

	return actor.NewActorSystem(systemName,
		actor.WithLogger(logger),
		actor.WithRemote(remoteCfg),
		actor.WithCluster(clusterCfg),
	)
}

// peerList parses a comma-separated peer list or returns this node's own
// discovery endpoint when raw is empty (single-node bootstrap).
func peerList(raw, selfHost string, selfPort int) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{fmt.Sprintf("%s:%d", selfHost, selfPort)}
	}

	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
