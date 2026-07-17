package main

// Level dimensions and rendering constants shared by the server and client.
const (
	// LevelW is the width of the playable level in tiles.
	LevelW = 100
	// LevelH is the height of the playable level in tiles.
	LevelH = 15
	// TilePx is the pixel size of one tile in the browser canvas.
	TilePx = 32
)

// MessageTypeInput is the JSON "type" field value for inbound player actions.
const MessageTypeInput = "input"

// MatchmakerActorName is the cluster-singleton actor name of the MatchFactory.
// The gateway resolves this via ActorOf to request a fresh game.
const MatchmakerActorName = "matchmaker"

// CreateMatch is sent by the gateway to the matchmaker when a WebSocket
// connects. The reply is a MatchCreated value.
type CreateMatch struct{}

// MatchCreated is the matchmaker reply to CreateMatch. The gateway uses
// MatchName to resolve the GameActor PID via ActorOf.
type MatchCreated struct {
	MatchName string `json:"matchName"`
}

// Player action strings — the wire-protocol contract between server and
// browser (web/main.ts). Keep both sides in sync when adding actions.
const (
	ActionLeft     = "left"
	ActionLeftEnd  = "left-end"
	ActionRight    = "right"
	ActionRightEnd = "right-end"
	ActionJump     = "jump"
	ActionJumpEnd  = "jump-end"
	ActionDown     = "down"
	ActionDownEnd  = "down-end"
	ActionPause    = "pause"
	ActionRestart  = "restart"
)

// PlayerInput is sent by the browser on key press and release.
// Type discriminates the message kind; Action is one of the Action* constants.
type PlayerInput struct {
	Type   string `json:"type"`
	Action string `json:"action"`
}

// Tile kind indices. Mirrored in web/main.ts as TILE.* constants.
const (
	TileAir = iota
	TileGround
	TileBrick
	TileQuestion
	TilePipe
	TileFlag
	TileCoin
)

// StageTheme selects the presentation palette for a campaign stage.
type StageTheme int

const (
	ThemeSunnyFields StageTheme = iota
	ThemePipeWorks
	ThemeEmberRuins
)

// MarioState is the player character position and velocity sent each tick.
type MarioState struct {
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	VX       float64 `json:"vx"`
	VY       float64 `json:"vy"`
	OnGround bool    `json:"onGround"`
	Facing   int     `json:"facing"` // -1 left, 1 right
	Dead     bool    `json:"dead"`
}

// EnemyState is a single enemy on the level. Kind 0 is a Goomba.
type EnemyState struct {
	ID    int     `json:"id"`
	Kind  int     `json:"kind"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	VY    float64 `json:"vy"`  // vertical velocity (server-side physics)
	Dir   int     `json:"dir"` // patrol direction: -1 left, 1 right
	Alive bool    `json:"alive"`
}

// Snapshot is the wire payload broadcast to the browser every tick.
type Snapshot struct {
	Tick         int          `json:"tick"`
	T            int64        `json:"t"`
	Tiles        [][]int8     `json:"tiles"`
	Mario        MarioState   `json:"mario"`
	Enemies      []EnemyState `json:"enemies"`
	CameraX      float64      `json:"cameraX"`
	Stage        int          `json:"stage"`       // zero-based current stage index
	TotalStages  int          `json:"totalStages"` // number of stages in the game
	World        int          `json:"world"`       // one-based campaign world number
	StageInWorld int          `json:"stageInWorld"`
	StageName    string       `json:"stageName"`
	Theme        StageTheme   `json:"theme"`
	StageClear   bool         `json:"stageClear"`
	Score        int          `json:"score"`
	Coins        int          `json:"coins"`
	Lives        int          `json:"lives"`
	TimeLeft     int          `json:"timeLeft"`
	GameOver     bool         `json:"gameOver"`
	Won          bool         `json:"won"`
	Paused       bool         `json:"paused"`
}

// tick is the internal scheduled message that drives the 60 Hz game loop.
type tick struct{}

// Subscribe registers the sender as a snapshot subscriber. The game uses
// ctx.Sender() rather than embedding a PID in the wire payload.
type Subscribe struct{}

// Unsubscribe removes a subscriber by session actor name. The gateway sends
// this on disconnect because Watch/*Terminated is unreliable cross-node.
type Unsubscribe struct {
	SessionName string `json:"sessionName"`
}
