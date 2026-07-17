package main

import (
	"math"
	"time"

	"github.com/tochemey/goakt/v4/actor"
)

const (
	tickInterval   = 16 * time.Millisecond // ~60 Hz
	schedRefPrefix = "tick."

	// Physics. A normal press clears the introductory platforms; holding Jump
	// reaches the taller optional paths in later stages.
	// height = jumpVel^2 / (2*gravity).
	gravity      = 0.07
	maxFallSpeed = 0.95
	runAccel     = 0.06
	runMaxSpeed  = 0.19
	runFriction  = 0.80
	jumpVel      = -0.86
	jumpCut      = 0.85 // upward velocity retained when jump is released early

	// Input forgiveness makes platforming reliable without enabling a
	// double-jump: a slightly early press is queued, and a jump just after
	// stepping off a ledge still counts.
	jumpBufferTicks = 6
	coyoteTimeTicks = 6

	marioW = 0.8
	marioH = 0.98

	enemyW      = 0.8
	enemyH      = 0.8
	goombaSpeed = 0.035

	coinScore   = 200
	goombaScore = 100
	flagScore   = 5000
	startLives  = 3
	levelTime   = 300

	// stompBounce is the upward velocity Mario gains after stomping an enemy.
	stompBounce = -0.45

	stageClearTicks = 120
)

// GameActor owns one Super Mario game: the tile map, Mario physics, enemies,
// score, current stage, and subscriber list. A recurring tick message advances
// the simulation at 60 Hz and broadcasts a Snapshot to every subscribed session.
type GameActor struct {
	tiles   [LevelH][LevelW]int8
	mario   MarioState
	enemies []EnemyState
	input   inputState

	stage           int
	score           int
	coins           int
	lives           int
	timeLeft        int
	gameOver        bool
	won             bool
	paused          bool
	stageClear      bool
	transitionTicks int

	spawnX float64
	spawnY float64
	flagX  float64

	previousMarioBottom float64
	jumpBuffer          int
	coyoteTime          int
	tickN               int
	subs                []*actor.PID
	schedRef            string
	hadSubscriber       bool
}

// inputState holds the current held-key state for continuous movement.
type inputState struct {
	left  bool
	right bool
	jump  bool
	down  bool
}

var _ actor.Actor = (*GameActor)(nil)

// PreStart is called before the actor begins receiving messages.
func (*GameActor) PreStart(*actor.Context) error { return nil }

// PostStop cancels the recurring physics tick schedule.
func (g *GameActor) PostStop(ctx *actor.Context) error {
	if g.schedRef != "" {
		_ = ctx.ActorSystem().CancelSchedule(g.schedRef)
	}
	return nil
}

// Receive dispatches actor lifecycle, subscription, input, and tick messages.
func (g *GameActor) Receive(ctx *actor.ReceiveContext) {
	switch msg := ctx.Message().(type) {
	case *actor.PostStart:
		g.reset()
		g.schedRef = schedRefPrefix + ctx.Self().Name()
		if err := ctx.ActorSystem().Schedule(ctx.Context(), &tick{}, ctx.Self(),
			tickInterval, actor.WithReference(g.schedRef)); err != nil {
			ctx.Err(err)
		}

	case *Subscribe:
		sender := ctx.Sender()
		g.subs = append(g.subs, sender)
		g.hadSubscriber = true
		ctx.Watch(sender)

	case *Unsubscribe:
		g.removeSubscriberByName(msg.SessionName)
		g.maybeShutdown(ctx)

	case *actor.Terminated:
		g.removeSubscriberByPath(msg.ActorPath())
		g.maybeShutdown(ctx)

	case *PlayerInput:
		g.handleInput(msg.Action)

	case *tick:
		if !g.gameOver && !g.won && !g.paused {
			g.step()
		}
		g.broadcast(ctx)

	default:
		ctx.Unhandled()
	}
}

// reset restarts the whole game from stage one with a fresh score and lives.
func (g *GameActor) reset() {
	g.stage = 0
	g.score = 0
	g.coins = 0
	g.lives = startLives
	g.gameOver = false
	g.won = false
	g.paused = false
	g.stageClear = false
	g.transitionTicks = 0
	g.tickN = 0
	g.input = inputState{}
	g.loadStage()
}

// loadStage loads the current stage's tiles, enemies, and spawn/flag markers
// and places Mario at the stage entrance.
func (g *GameActor) loadStage() {
	lv, def := buildStage(g.stage)
	g.tiles = lv.Tiles
	g.enemies = lv.Enemies
	g.spawnX = lv.SpawnX
	g.spawnY = lv.SpawnY
	g.flagX = lv.FlagX
	g.mario = MarioState{X: lv.SpawnX, Y: lv.SpawnY, Facing: 1}
	g.previousMarioBottom = lv.SpawnY + marioH
	g.jumpBuffer = 0
	g.coyoteTime = 0
	g.timeLeft = def.TimeLimit
	if g.timeLeft <= 0 {
		g.timeLeft = levelTime
	}
}

// handleInput updates held-key state and handles edge-triggered actions such
// as jump, pause, and restart.
func (g *GameActor) handleInput(action string) {
	switch action {
	case ActionPause:
		if !g.gameOver && !g.won {
			g.paused = !g.paused
		}
	case ActionRestart:
		if g.gameOver || g.won {
			g.reset()
		}
	case ActionLeft:
		g.input.left = true
	case ActionRight:
		g.input.right = true
	case ActionJump:
		g.input.jump = true
		// Edge-triggered: queue exactly one jump press. step consumes this
		// when Mario is grounded or within the coyote-time window.
		g.jumpBuffer = jumpBufferTicks
	case ActionDown:
		g.input.down = true
	case ActionLeftEnd:
		g.input.left = false
	case ActionRightEnd:
		g.input.right = false
	case ActionJumpEnd:
		g.input.jump = false
		// Variable jump height: releasing early trims upward velocity.
		if g.mario.VY < 0 {
			g.mario.VY *= jumpCut
		}
	case ActionDownEnd:
		g.input.down = false
	}
}

// step advances the simulation by one tick: timer, input, physics, enemies.
func (g *GameActor) step() {
	g.tickN++
	g.previousMarioBottom = g.mario.Y + marioH

	if g.stageClear {
		g.transitionTicks--
		if g.transitionTicks <= 0 {
			g.stage++
			g.stageClear = false
			g.loadStage()
		}
		return
	}

	if g.tickN%60 == 0 {
		g.timeLeft--
		if g.timeLeft <= 0 {
			g.killMario()
			return
		}
	}

	g.applyMarioInput()
	g.applyJumpInput()

	g.mario.VY += gravity
	if g.mario.VY > maxFallSpeed {
		g.mario.VY = maxFallSpeed
	}

	// Axis-separated movement: resolve X first, then Y.
	g.moveMarioX(g.mario.VX)
	g.mario.OnGround = false
	g.moveMarioY(g.mario.VY)

	g.collectCoins()
	g.checkFallDeath()

	g.updateEnemies()
	g.checkEnemyCollisions()
	g.checkFlag()
}

// applyJumpInput consumes a buffered jump while Mario is on the ground or
// shortly after leaving a ledge. It intentionally never fires a second jump
// while airborne.
func (g *GameActor) applyJumpInput() {
	if g.mario.OnGround {
		g.coyoteTime = coyoteTimeTicks
	} else if g.coyoteTime > 0 {
		g.coyoteTime--
	}

	if g.jumpBuffer > 0 {
		g.jumpBuffer--
	}

	if g.mario.Dead || g.jumpBuffer == 0 || g.coyoteTime == 0 {
		return
	}

	g.mario.VY = jumpVel
	g.mario.OnGround = false
	g.jumpBuffer = 0
	g.coyoteTime = 0
}

// applyMarioInput applies horizontal acceleration and friction from held keys.
func (g *GameActor) applyMarioInput() {
	if g.mario.Dead {
		return
	}

	switch {
	case g.input.left && !g.input.right:
		g.mario.VX -= runAccel
		g.mario.Facing = -1
	case g.input.right && !g.input.left:
		g.mario.VX += runAccel
		g.mario.Facing = 1
	default:
		g.mario.VX *= runFriction
		if math.Abs(g.mario.VX) < 0.004 {
			g.mario.VX = 0
		}
	}

	if g.mario.VX > runMaxSpeed {
		g.mario.VX = runMaxSpeed
	}
	if g.mario.VX < -runMaxSpeed {
		g.mario.VX = -runMaxSpeed
	}
}

// moveMarioX moves Mario horizontally and resolves collisions against the
// tiles his body actually overlaps.
func (g *GameActor) moveMarioX(dx float64) {
	if dx == 0 {
		return
	}
	g.mario.X += dx
	// Keep Mario inside the level horizontally.
	if g.mario.X < 0 {
		g.mario.X = 0
		g.mario.VX = 0
	}
	if g.mario.X+marioW > LevelW {
		g.mario.X = LevelW - marioW
		g.mario.VX = 0
	}
	if boxHitsSolid(g, g.mario.X, g.mario.Y, marioW, marioH) {
		if dx > 0 {
			g.mario.X = math.Floor(g.mario.X+marioW) - marioW
		} else {
			g.mario.X = math.Floor(g.mario.X) + 1
		}
		g.mario.VX = 0
	}
}

// moveMarioY moves Mario vertically and resolves floor/ceiling collisions.
func (g *GameActor) moveMarioY(dy float64) {
	if dy == 0 {
		return
	}
	g.mario.Y += dy
	if !boxHitsSolid(g, g.mario.X, g.mario.Y, marioW, marioH) {
		return
	}
	if dy > 0 {
		// Landing on a floor.
		g.mario.Y = math.Floor(g.mario.Y+marioH) - marioH
		g.mario.VY = 0
		g.mario.OnGround = true
	} else {
		// Bumping a ceiling.
		hitRow := int(math.Floor(g.mario.Y))
		g.mario.Y = float64(hitRow + 1)
		g.mario.VY = 0
		g.hitBlockFromBelow(hitRow)
	}
}

// boxHitsSolid reports whether an axis-aligned box at (x, y) with size (w, h)
// overlaps any solid tile. Only the tiles the box spans are examined.
func boxHitsSolid(g *GameActor, x, y, w, h float64) bool {
	left := int(math.Floor(x))
	right := int(math.Floor(x + w - 1e-6))
	top := int(math.Floor(y))
	bottom := int(math.Floor(y + h - 1e-6))

	for ty := top; ty <= bottom; ty++ {
		for tx := left; tx <= right; tx++ {
			if g.isSolid(tx, ty) {
				return true
			}
		}
	}
	return false
}

// isSolid reports whether the tile at (tx, ty) blocks movement. Tiles outside
// the level are treated as open air so Mario can fall into pits.
func (g *GameActor) isSolid(tx, ty int) bool {
	if tx < 0 || tx >= LevelW || ty < 0 || ty >= LevelH {
		return false
	}
	switch g.tiles[ty][tx] {
	case TileGround, TileBrick, TileQuestion, TilePipe:
		return true
	default:
		return false
	}
}

// hitBlockFromBelow turns question blocks in hitRow into bricks and awards a
// coin. hitRow is captured before Mario is snapped beneath the ceiling.
func (g *GameActor) hitBlockFromBelow(hitRow int) {
	left := int(math.Floor(g.mario.X))
	right := int(math.Floor(g.mario.X + marioW - 1e-6))
	for tx := left; tx <= right; tx++ {
		if tx < 0 || tx >= LevelW || hitRow < 0 || hitRow >= LevelH {
			continue
		}
		if g.tiles[hitRow][tx] == TileQuestion {
			g.tiles[hitRow][tx] = TileBrick
			g.coins++
			g.score += coinScore
		}
	}
}

// collectCoins removes coin tiles overlapping Mario's body and scores them.
func (g *GameActor) collectCoins() {
	left := int(math.Floor(g.mario.X))
	right := int(math.Floor(g.mario.X + marioW - 1e-6))
	top := int(math.Floor(g.mario.Y))
	bottom := int(math.Floor(g.mario.Y + marioH - 1e-6))
	for ty := top; ty <= bottom; ty++ {
		for tx := left; tx <= right; tx++ {
			if tx >= 0 && tx < LevelW && ty >= 0 && ty < LevelH && g.tiles[ty][tx] == TileCoin {
				g.tiles[ty][tx] = TileAir
				g.coins++
				g.score += coinScore
			}
		}
	}
}

// checkFallDeath kills Mario when he falls below the level.
func (g *GameActor) checkFallDeath() {
	if g.mario.Y > float64(LevelH) {
		g.killMario()
	}
}

// killMario decrements lives and respawns at the stage entrance, or ends the
// game when no lives remain.
func (g *GameActor) killMario() {
	g.lives--
	if g.lives <= 0 {
		g.gameOver = true
		g.mario.Dead = true
		g.mario.VX = 0
		g.mario.VY = 0
		return
	}
	g.mario = MarioState{X: g.spawnX, Y: g.spawnY, Facing: 1}
	g.timeLeft = levelTime
}

// updateEnemies moves Goombas with gravity, wall bounce, and ledge turning so
// they patrol platforms instead of walking off edges.
func (g *GameActor) updateEnemies() {
	for i := range g.enemies {
		e := &g.enemies[i]
		if !e.Alive {
			continue
		}

		// Gravity + vertical resolution.
		e.VY += gravity
		if e.VY > maxFallSpeed {
			e.VY = maxFallSpeed
		}
		e.Y += e.VY
		if boxHitsSolid(g, e.X, e.Y, enemyW, enemyH) {
			if e.VY > 0 {
				e.Y = math.Floor(e.Y+enemyH) - enemyH
			} else {
				e.Y = math.Floor(e.Y) + 1
			}
			e.VY = 0
		}

		if e.Dir == 0 {
			e.Dir = -1
		}

		// Horizontal patrol with wall bounce and ledge turn-around.
		nextX := e.X + float64(e.Dir)*goombaSpeed
		switch {
		case nextX < 0 || nextX+enemyW > LevelW:
			e.Dir = -e.Dir
		case boxHitsSolid(g, nextX, e.Y, enemyW, enemyH):
			e.Dir = -e.Dir
		default:
			// Look for ground just ahead of the leading foot; turn if none.
			aheadX := nextX + enemyW/2 + float64(e.Dir)*(enemyW/2+0.05)
			belowY := e.Y + enemyH + 0.05
			if e.VY == 0 && !g.isSolid(int(math.Floor(aheadX)), int(math.Floor(belowY))) {
				e.Dir = -e.Dir
			} else {
				e.X = nextX
			}
		}

		if e.Y > float64(LevelH)+2 {
			e.Alive = false
		}
	}
}

// checkEnemyCollisions handles stomps and fatal side hits. A stomp occurs
// when Mario is descending and his feet cross an enemy's head between ticks.
// Recording the prior foot position prevents a fast fall from skipping the
// narrow overlap range that would otherwise register the stomp.
func (g *GameActor) checkEnemyCollisions() {
	if g.mario.Dead {
		return
	}
	mx1, my1 := g.mario.X, g.mario.Y
	mx2, my2 := mx1+marioW, my1+marioH

	for i := range g.enemies {
		e := &g.enemies[i]
		if !e.Alive {
			continue
		}
		ex1, ey1 := e.X, e.Y
		ex2, ey2 := ex1+enemyW, ey1+enemyH

		if mx2 <= ex1 || mx1 >= ex2 || my2 <= ey1 || my1 >= ey2 {
			continue
		}

		// A descending Mario whose feet were at or above the enemy's head on
		// the prior tick has landed on it. The small tolerance absorbs
		// sub-tile rounding at the collision boundary.
		descendedOntoEnemy := g.mario.VY >= 0 &&
			g.previousMarioBottom <= ey1+0.05 &&
			my2 >= ey1
		if descendedOntoEnemy {
			e.Alive = false
			g.mario.VY = stompBounce
			g.mario.OnGround = false
			g.score += goombaScore
			continue
		}

		g.killMario()
		return
	}
}

// checkFlag advances to the next stage when Mario reaches the flag, or wins
// the game after the final stage.
func (g *GameActor) checkFlag() {
	if g.mario.X < g.flagX-1 {
		return
	}
	g.score += flagScore + g.timeLeft*10
	if g.stage < len(campaign)-1 {
		g.stageClear = true
		g.transitionTicks = stageClearTicks
		g.mario.VX = 0
		g.mario.VY = 0
		return
	}
	g.won = true
}

// cameraX returns the horizontal scroll offset that keeps Mario roughly
// centered while clamping to the level bounds.
func (g *GameActor) cameraX() float64 {
	cam := g.mario.X - 9
	if cam < 0 {
		cam = 0
	}
	maxCam := float64(LevelW) - 20
	if cam > maxCam {
		cam = maxCam
	}
	return cam
}

// removeSubscriberByName drops a subscriber identified by session actor name.
func (g *GameActor) removeSubscriberByName(name string) {
	for i, p := range g.subs {
		if p.Name() == name {
			g.subs = append(g.subs[:i], g.subs[i+1:]...)
			return
		}
	}
}

// removeSubscriberByPath drops a subscriber identified by actor path.
func (g *GameActor) removeSubscriberByPath(path actor.Path) {
	for i, p := range g.subs {
		if p.Path().Equals(path) {
			g.subs = append(g.subs[:i], g.subs[i+1:]...)
			return
		}
	}
}

// maybeShutdown stops the game when all subscribers have disconnected.
func (g *GameActor) maybeShutdown(ctx *actor.ReceiveContext) {
	if g.hadSubscriber && len(g.subs) == 0 {
		ctx.Shutdown()
	}
}

// broadcast sends the current Snapshot to every subscribed session.
func (g *GameActor) broadcast(ctx *actor.ReceiveContext) {
	def := stageDefinition(g.stage)
	snap := &Snapshot{
		Tick:         g.tickN,
		T:            time.Now().UnixMilli(),
		Tiles:        g.tilesCopy(),
		Mario:        g.mario,
		Enemies:      append([]EnemyState(nil), g.enemies...),
		CameraX:      g.cameraX(),
		Stage:        g.stage,
		TotalStages:  len(campaign),
		World:        def.World,
		StageInWorld: def.Stage,
		StageName:    def.Name,
		Theme:        def.Theme,
		StageClear:   g.stageClear,
		Score:        g.score,
		Coins:        g.coins,
		Lives:        g.lives,
		TimeLeft:     g.timeLeft,
		GameOver:     g.gameOver,
		Won:          g.won,
		Paused:       g.paused,
	}
	for _, sub := range g.subs {
		ctx.Tell(sub, snap)
	}
}

// tilesCopy returns a deep copy of the tile grid for the wire payload.
func (g *GameActor) tilesCopy() [][]int8 {
	t := make([][]int8, LevelH)
	for r := 0; r < LevelH; r++ {
		t[r] = make([]int8, LevelW)
		copy(t[r], g.tiles[r][:])
	}
	return t
}
