# Skybound Runner — Browser Game powered by GoAkt

![Skybound Runner game logo](assets/skybound-runner-game-logo.png)

An original browser-playable retro platformer built on [GoAkt](https://github.com/Tochemey/goakt). Every game session is a per-WebSocket-connection actor; game state snapshots stream to the browser at 60 Hz over WebSocket.

The game contains a 10-stage campaign across Sunny Fields, Pipe Works, and Ember Ruins. Its Canvas characters, effects, and looping Web Audio chiptunes are original procedural assets; it does not ship third-party game art, music, or level data.

## Architecture

```ascii

Browser (Canvas)  ←── WebSocket ──→  PlayerSessionActor
                                           │
                                           ▼ Tell
                                      GameActor (60 Hz tick)
                                           ▲
                                           │ SpawnOn
                                    MatchFactory (singleton)
```

- **GameActor** — owns Mario physics, enemies, collisions, scoring, and the level
- **PlayerSessionActor** — bridges one WebSocket to the game actor
- **MatchFactory** — cluster singleton that spawns a fresh game per connection
- **Gateway** — HTTP/WebSocket upgrade and per-connection lifecycle

## Quick start

```bash
make run
```

Open [http://localhost:8080](http://localhost:8080)

## Controls

| Key            | Action                           |
|----------------|----------------------------------|
| ← / → or A / D | Move                             |
| ↑ / W / Space  | Jump                             |
| P              | Pause                            |
| F              | Toggle browser fullscreen        |
| M              | Toggle sound                     |
| R              | Restart (after game over or win) |

## Requirements

- Go 1.26+
- Node.js 18+ and pnpm 10+ (only needed to compile the TypeScript client)
- GNU Make (for the provided build targets)

## Development

```bash
# Recompile web client only
make web

# Build binary
make build

# Run server
make run
```

## Validation

Run the following on Windows, Linux, or macOS:

```bash
pnpm run build
go vet ./...
go test ./...
make build
```

## GoAkt features demonstrated

| Feature              | Usage                                   |
|----------------------|-----------------------------------------|
| Scheduled tick loop  | `system.Schedule` drives 60 Hz physics  |
| Per-connection actor | `PlayerSessionActor` per WebSocket      |
| Watch / Terminated   | Game cleans up when session disconnects |
| SpawnSingleton       | `MatchFactory` matchmaker               |
| SpawnOn + LeastLoad  | Cluster-aware game placement            |
| CBOR serializers     | Cross-node message types                |

## License

MIT
