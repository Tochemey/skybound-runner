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

	// Holding Down while airborne fast-falls; while grounded it crouches.
	fastFallGravity  = 0.13
	fastFallMaxSpeed = 1.25
	crouchH          = 0.6

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
	flyerSpeed  = 0.03
	flyerRange  = 2.5 // horizontal patrol half-width around BaseX
	flyerBob    = 1.1 // vertical sine amplitude around BaseY

	itemW     = 0.7
	itemH     = 0.7
	itemSpeed = 0.05

	coinScore    = 200
	goombaScore  = 100
	shieldScore  = 1000
	flagScore    = 5000
	startLives   = 3
	levelTime    = 300
	coinsPerLife = 100

	// stompBounce is the upward velocity Mario gains after stomping an enemy.
	stompBounce = -0.45

	stageClearTicks = 120
	deathAnimTicks  = 50  // tumble duration before a death is finalized
	squashTicks     = 36  // how long a stomped enemy stays squashed on screen
	shieldInvuln    = 120 // invulnerability ticks after a shield absorbs a hit
)

// playerState is one connected player: their runner, held inputs, and the
// session actor that receives their personalized snapshots.
type playerState struct {
	name       string
	pid        *actor.PID
	mario      MarioState
	input      inputState
	jumpBuffer int
	coyoteTime int
	prevBottom float64
	deathTicks int  // >0 while the death tumble animation runs
	needsTiles bool // next snapshot must carry the full tile grid
}

// height is the player's current collision height (crouching shrinks it).
func (p *playerState) height() float64 {
	if p.mario.Crouch {
		return crouchH
	}
	return marioH
}

// GameActor owns one co-op game: the tile map, every player's physics,
// enemies, items, score, current stage, and the subscriber list. A recurring
// tick message advances the simulation at 60 Hz and broadcasts a personalized
// Snapshot to every subscribed session.
type GameActor struct {
	tiles   [LevelH][LevelW]int8
	players []*playerState
	enemies []EnemyState
	items   []ItemState

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

	checkpointX      float64 // -1 when the stage has none
	checkpointActive bool

	tilesDirty bool // grid changed since the last broadcast that carried tiles
	nextItemID int

	tickN         int
	schedRef      string
	hadSubscriber bool
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
		g.addPlayer(sender.Name(), sender)
		g.hadSubscriber = true
		ctx.Watch(sender)

	case *Unsubscribe:
		g.removePlayer(func(p *playerState) bool { return p.name == msg.SessionName })
		g.maybeShutdown(ctx)

	case *actor.Terminated:
		g.removePlayer(func(p *playerState) bool { return p.pid != nil && p.pid.Path().Equals(msg.ActorPath()) })
		g.maybeShutdown(ctx)

	case *PlayerInput:
		g.handleInput(g.playerBySender(ctx.Sender()), msg.Action)

	case *tick:
		if !g.gameOver && !g.won && !g.paused {
			g.step()
		}
		g.broadcast(ctx)

	default:
		ctx.Unhandled()
	}
}

// addPlayer registers a new subscriber and drops their runner at the spawn.
func (g *GameActor) addPlayer(name string, pid *actor.PID) {
	p := &playerState{
		name:       name,
		pid:        pid,
		mario:      MarioState{X: g.spawnX, Y: g.spawnY, Facing: 1},
		needsTiles: true,
	}
	p.prevBottom = p.mario.Y + marioH
	g.players = append(g.players, p)
}

// removePlayer drops the first player matching the predicate.
func (g *GameActor) removePlayer(match func(*playerState) bool) {
	for i, p := range g.players {
		if match(p) {
			g.players = append(g.players[:i], g.players[i+1:]...)
			return
		}
	}
}

// playerBySender resolves an input message to the sending player's state.
// Falls back to the first player so a lone-player game never drops input
// even if sender metadata is missing.
func (g *GameActor) playerBySender(sender *actor.PID) *playerState {
	if sender != nil {
		for _, p := range g.players {
			if p.name == sender.Name() {
				return p
			}
		}
	}
	if len(g.players) > 0 {
		return g.players[0]
	}
	return nil
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
	g.loadStage()
}

// loadStage loads the current stage's tiles, enemies, and spawn/flag markers
// and places every player at the stage entrance.
func (g *GameActor) loadStage() {
	lv, def := buildStage(g.stage)
	g.tiles = lv.Tiles
	g.enemies = lv.Enemies
	g.items = nil
	g.spawnX = lv.SpawnX
	g.spawnY = lv.SpawnY
	g.flagX = lv.FlagX
	g.placeCheckpoint(def)
	for _, p := range g.players {
		g.respawnPlayer(p)
	}
	g.timeLeft = def.TimeLimit
	if g.timeLeft <= 0 {
		g.timeLeft = levelTime
	}
	g.tilesDirty = true
}

// placeCheckpoint finds a clear surface column near mid-stage and plants a
// checkpoint marker there. Stages with no suitable column get none.
func (g *GameActor) placeCheckpoint(def StageDef) {
	g.checkpointX = -1
	g.checkpointActive = false
	mid := def.FlagCol / 2
	for offset := 0; offset <= 10; offset++ {
		for _, col := range []int{mid + offset, mid - offset} {
			if col < 2 || col >= LevelW-2 {
				continue
			}
			if g.tiles[groundTopRow][col] == TileGround &&
				g.tiles[surfaceRow][col] == TileAir &&
				g.tiles[surfaceRow-1][col] == TileAir {
				g.tiles[surfaceRow][col] = TileCheckpoint
				g.checkpointX = float64(col)
				return
			}
		}
	}
}

// respawnPlayer places a player at the active spawn point, alive and clean.
func (g *GameActor) respawnPlayer(p *playerState) {
	x, y := g.spawnX, g.spawnY
	if g.checkpointActive {
		x, y = g.checkpointX, float64(surfaceRow-1)
	}
	p.mario = MarioState{X: x, Y: y, Facing: 1}
	p.prevBottom = y + marioH
	p.jumpBuffer = 0
	p.coyoteTime = 0
	p.deathTicks = 0
}

// handleInput updates held-key state and handles edge-triggered actions such
// as jump, pause, and restart.
func (g *GameActor) handleInput(p *playerState, action string) {
	if p == nil {
		return
	}
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
		p.input.left = true
	case ActionRight:
		p.input.right = true
	case ActionJump:
		p.input.jump = true
		// Edge-triggered: queue exactly one jump press. step consumes this
		// when the runner is grounded or within the coyote-time window.
		p.jumpBuffer = jumpBufferTicks
	case ActionDown:
		p.input.down = true
	case ActionLeftEnd:
		p.input.left = false
	case ActionRightEnd:
		p.input.right = false
	case ActionJumpEnd:
		p.input.jump = false
		// Variable jump height: releasing early trims upward velocity.
		if p.mario.VY < 0 {
			p.mario.VY *= jumpCut
		}
	case ActionDownEnd:
		p.input.down = false
	}
}

// step advances the simulation by one tick: timer, input, physics, enemies.
func (g *GameActor) step() {
	g.tickN++

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
			for _, p := range g.players {
				g.killPlayer(p, true)
			}
		}
	}

	for _, p := range g.players {
		g.stepPlayer(p)
	}

	g.updateItems()
	g.updateEnemies()
	for _, p := range g.players {
		g.checkEnemyCollisions(p)
	}
	g.checkCheckpoint()
	g.checkFlag()
}

// stepPlayer advances one player's physics, or their death tumble.
func (g *GameActor) stepPlayer(p *playerState) {
	p.prevBottom = p.mario.Y + p.height()

	if p.mario.Invuln > 0 {
		p.mario.Invuln--
	}

	if p.mario.Dead {
		// Death tumble: a small pop then a fall through the world, no
		// collision. finalizeDeath runs when the animation completes.
		if p.deathTicks > 0 {
			p.deathTicks--
			p.mario.VY += gravity
			p.mario.Y += p.mario.VY
			if p.deathTicks == 0 {
				g.finalizeDeath(p)
			}
		}
		return
	}

	g.applyCrouch(p)
	g.applyMarioInput(p)
	g.applyJumpInput(p)

	// Fast-fall: holding Down while airborne drops harder.
	grav, maxFall := gravity, maxFallSpeed
	if p.input.down && !p.mario.OnGround {
		grav, maxFall = fastFallGravity, fastFallMaxSpeed
	}
	p.mario.VY += grav
	if p.mario.VY > maxFall {
		p.mario.VY = maxFall
	}

	// Axis-separated movement: resolve X first, then Y.
	g.moveMarioX(p, p.mario.VX)
	p.mario.OnGround = false
	g.moveMarioY(p, p.mario.VY)

	g.collectCoins(p)
	g.checkFallDeath(p)
}

// applyCrouch enters a crouch when Down is held on the ground, and stands
// back up only when there is headroom for the full hitbox.
func (g *GameActor) applyCrouch(p *playerState) {
	if p.input.down && p.mario.OnGround {
		if !p.mario.Crouch {
			// Feet stay planted: the top of the hitbox moves down.
			p.mario.Y += marioH - crouchH
			p.mario.Crouch = true
		}
		return
	}
	if p.mario.Crouch {
		standY := p.mario.Y - (marioH - crouchH)
		if !boxHitsSolid(g, p.mario.X, standY, marioW, marioH) {
			p.mario.Y = standY
			p.mario.Crouch = false
		}
	}
}

// applyJumpInput consumes a buffered jump while the runner is on the ground
// or shortly after leaving a ledge. It intentionally never fires a second
// jump while airborne, and never while crouching.
func (g *GameActor) applyJumpInput(p *playerState) {
	if p.mario.OnGround {
		p.coyoteTime = coyoteTimeTicks
	} else if p.coyoteTime > 0 {
		p.coyoteTime--
	}

	if p.jumpBuffer > 0 {
		p.jumpBuffer--
	}

	if p.mario.Dead || p.mario.Crouch || p.jumpBuffer == 0 || p.coyoteTime == 0 {
		return
	}

	p.mario.VY = jumpVel
	p.mario.OnGround = false
	p.jumpBuffer = 0
	p.coyoteTime = 0
}

// applyMarioInput applies horizontal acceleration and friction from held keys.
func (g *GameActor) applyMarioInput(p *playerState) {
	if p.mario.Dead {
		return
	}

	switch {
	case p.mario.Crouch:
		// Crouching: no drive, strong friction.
		p.mario.VX *= runFriction
		if math.Abs(p.mario.VX) < 0.004 {
			p.mario.VX = 0
		}
	case p.input.left && !p.input.right:
		p.mario.VX -= runAccel
		p.mario.Facing = -1
	case p.input.right && !p.input.left:
		p.mario.VX += runAccel
		p.mario.Facing = 1
	default:
		p.mario.VX *= runFriction
		if math.Abs(p.mario.VX) < 0.004 {
			p.mario.VX = 0
		}
	}

	if p.mario.VX > runMaxSpeed {
		p.mario.VX = runMaxSpeed
	}
	if p.mario.VX < -runMaxSpeed {
		p.mario.VX = -runMaxSpeed
	}
}

// moveMarioX moves a player horizontally and resolves collisions against the
// tiles their body actually overlaps.
func (g *GameActor) moveMarioX(p *playerState, dx float64) {
	if dx == 0 {
		return
	}
	p.mario.X += dx
	// Keep the runner inside the level horizontally.
	if p.mario.X < 0 {
		p.mario.X = 0
		p.mario.VX = 0
	}
	if p.mario.X+marioW > LevelW {
		p.mario.X = LevelW - marioW
		p.mario.VX = 0
	}
	if boxHitsSolid(g, p.mario.X, p.mario.Y, marioW, p.height()) {
		if dx > 0 {
			p.mario.X = math.Floor(p.mario.X+marioW) - marioW
		} else {
			p.mario.X = math.Floor(p.mario.X) + 1
		}
		p.mario.VX = 0
	}
}

// moveMarioY moves a player vertically and resolves floor/ceiling collisions.
func (g *GameActor) moveMarioY(p *playerState, dy float64) {
	if dy == 0 {
		return
	}
	p.mario.Y += dy
	if !boxHitsSolid(g, p.mario.X, p.mario.Y, marioW, p.height()) {
		return
	}
	if dy > 0 {
		// Landing on a floor.
		p.mario.Y = math.Floor(p.mario.Y+p.height()) - p.height()
		p.mario.VY = 0
		p.mario.OnGround = true
	} else {
		// Bumping a ceiling.
		hitRow := int(math.Floor(p.mario.Y))
		p.mario.Y = float64(hitRow + 1)
		p.mario.VY = 0
		g.hitBlockFromBelow(p, hitRow)
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
// the level are treated as open air so players can fall into pits.
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

// blockHoldsShield decides deterministically whether a question block hides a
// shield power-up instead of a coin. Roughly one in five does.
func blockHoldsShield(tx, ty int) bool {
	return (tx*31+ty*17)%5 == 2
}

// hitBlockFromBelow resolves question blocks in hitRow: most pay a coin, some
// release a shield power-up that slides along the ground until collected.
// hitRow is captured before the player is snapped beneath the ceiling.
func (g *GameActor) hitBlockFromBelow(p *playerState, hitRow int) {
	left := int(math.Floor(p.mario.X))
	right := int(math.Floor(p.mario.X + marioW - 1e-6))
	for tx := left; tx <= right; tx++ {
		if tx < 0 || tx >= LevelW || hitRow < 0 || hitRow >= LevelH {
			continue
		}
		if g.tiles[hitRow][tx] != TileQuestion {
			continue
		}
		g.tiles[hitRow][tx] = TileBrick
		g.tilesDirty = true
		if blockHoldsShield(tx, hitRow) {
			g.spawnItem(ItemShield, float64(tx), float64(hitRow-1))
		} else {
			g.addCoin()
			g.score += coinScore
		}
	}
}

// spawnItem pops a power-up out of a block; it then slides like a mushroom.
func (g *GameActor) spawnItem(kind int, x, y float64) {
	g.nextItemID++
	g.items = append(g.items, ItemState{
		ID:    g.nextItemID,
		Kind:  kind,
		X:     x,
		Y:     y,
		Dir:   1,
		Alive: true,
	})
}

// updateItems applies gravity and horizontal drift to live items, bounces
// them off walls, and grants their power to a player that touches them.
func (g *GameActor) updateItems() {
	for i := range g.items {
		it := &g.items[i]
		if !it.Alive {
			continue
		}

		it.VY += gravity
		if it.VY > maxFallSpeed {
			it.VY = maxFallSpeed
		}
		it.Y += it.VY
		if boxHitsSolid(g, it.X, it.Y, itemW, itemH) {
			if it.VY > 0 {
				it.Y = math.Floor(it.Y+itemH) - itemH
			} else {
				it.Y = math.Floor(it.Y) + 1
			}
			it.VY = 0
		}

		nextX := it.X + float64(it.Dir)*itemSpeed
		if nextX < 0 || nextX+itemW > LevelW || boxHitsSolid(g, nextX, it.Y, itemW, itemH) {
			it.Dir = -it.Dir
		} else {
			it.X = nextX
		}

		if it.Y > float64(LevelH)+2 {
			it.Alive = false
			continue
		}

		for _, p := range g.players {
			if p.mario.Dead {
				continue
			}
			if overlaps(p.mario.X, p.mario.Y, marioW, p.height(), it.X, it.Y, itemW, itemH) {
				it.Alive = false
				p.mario.Shield = true
				g.score += shieldScore
				break
			}
		}
	}
}

// addCoin increments the coin counter; every full hundred converts to a life.
func (g *GameActor) addCoin() {
	g.coins++
	if g.coins >= coinsPerLife {
		g.coins -= coinsPerLife
		g.lives++
	}
}

// collectCoins removes coin tiles overlapping a player's body and scores them.
func (g *GameActor) collectCoins(p *playerState) {
	left := int(math.Floor(p.mario.X))
	right := int(math.Floor(p.mario.X + marioW - 1e-6))
	top := int(math.Floor(p.mario.Y))
	bottom := int(math.Floor(p.mario.Y + p.height() - 1e-6))
	for ty := top; ty <= bottom; ty++ {
		for tx := left; tx <= right; tx++ {
			if tx >= 0 && tx < LevelW && ty >= 0 && ty < LevelH && g.tiles[ty][tx] == TileCoin {
				g.tiles[ty][tx] = TileAir
				g.tilesDirty = true
				g.addCoin()
				g.score += coinScore
			}
		}
	}
}

// checkFallDeath kills a player when they fall below the level.
func (g *GameActor) checkFallDeath(p *playerState) {
	if p.mario.Y > float64(LevelH) {
		g.killPlayer(p, true)
	}
}

// killPlayer starts a death unless the player is invulnerable or shielded.
// force bypasses both (pits and the stage timer are always fatal).
func (g *GameActor) killPlayer(p *playerState, force bool) {
	if p.mario.Dead {
		return
	}
	if !force {
		if p.mario.Invuln > 0 {
			return
		}
		if p.mario.Shield {
			p.mario.Shield = false
			p.mario.Invuln = shieldInvuln
			return
		}
	}
	p.mario.Dead = true
	p.mario.Crouch = false
	p.mario.VX = 0
	p.mario.VY = -0.5 // small pop before the tumble
	p.deathTicks = deathAnimTicks
}

// finalizeDeath runs when a death tumble ends: it consumes a life and either
// respawns the player or ends the game when the shared pool is empty.
func (g *GameActor) finalizeDeath(p *playerState) {
	g.lives--
	if g.lives <= 0 {
		g.gameOver = true
		for _, other := range g.players {
			other.mario.Dead = true
			other.mario.VX = 0
			other.mario.VY = 0
			other.deathTicks = 0
		}
		return
	}
	g.respawnPlayer(p)
	def := stageDefinition(g.stage)
	g.timeLeft = def.TimeLimit
	if g.timeLeft <= 0 {
		g.timeLeft = levelTime
	}
}

// updateEnemies advances every live enemy and counts down squash animations.
func (g *GameActor) updateEnemies() {
	for i := range g.enemies {
		e := &g.enemies[i]
		if !e.Alive {
			if e.Dying > 0 {
				e.Dying--
			}
			continue
		}

		if e.Kind == EnemyFlyer {
			g.updateFlyer(e)
			continue
		}
		g.updateWalker(e)
	}
}

// updateFlyer hovers an enemy on a sine wave around its patrol anchor.
func (g *GameActor) updateFlyer(e *EnemyState) {
	if e.Dir == 0 {
		e.Dir = 1
	}
	e.X += float64(e.Dir) * flyerSpeed
	minX := math.Max(0, e.BaseX-flyerRange)
	maxX := math.Min(float64(LevelW)-enemyW, e.BaseX+flyerRange)
	if e.X <= minX {
		e.X = minX
		e.Dir = 1
	} else if e.X >= maxX {
		e.X = maxX
		e.Dir = -1
	}
	// Phase offset by ID so flyers don't bob in lockstep.
	e.Y = e.BaseY + math.Sin(float64(g.tickN)*0.045+float64(e.ID)*1.3)*flyerBob
}

// updateWalker moves ground enemies with gravity, wall bounce, and ledge
// turning so they patrol platforms instead of walking off edges.
func (g *GameActor) updateWalker(e *EnemyState) {
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

// overlaps reports whether two axis-aligned boxes intersect.
func overlaps(ax, ay, aw, ah, bx, by, bw, bh float64) bool {
	return ax < bx+bw && ax+aw > bx && ay < by+bh && ay+ah > by
}

// checkEnemyCollisions handles stomps and harmful hits for one player. A
// stomp occurs when the player is descending and their feet cross an enemy's
// head between ticks. Recording the prior foot position prevents a fast fall
// from skipping the narrow overlap range that would otherwise register the
// stomp. Spiky enemies can never be stomped — all contact hurts.
func (g *GameActor) checkEnemyCollisions(p *playerState) {
	if p.mario.Dead {
		return
	}
	h := p.height()
	mx1, my1 := p.mario.X, p.mario.Y
	mx2, my2 := mx1+marioW, my1+h

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

		// A descending player whose feet were at or above the enemy's head on
		// the prior tick has landed on it. The small tolerance absorbs
		// sub-tile rounding at the collision boundary.
		descendedOntoEnemy := p.mario.VY >= 0 &&
			p.prevBottom <= ey1+0.05 &&
			my2 >= ey1
		if descendedOntoEnemy && e.Kind != EnemySpiky {
			e.Alive = false
			e.Dying = squashTicks
			p.mario.VY = stompBounce
			p.mario.OnGround = false
			g.score += goombaScore
			continue
		}

		g.killPlayer(p, false)
		return
	}
}

// checkCheckpoint activates the mid-stage checkpoint when any live player
// reaches it; respawns then use the checkpoint instead of the stage entrance.
func (g *GameActor) checkCheckpoint() {
	if g.checkpointActive || g.checkpointX < 0 {
		return
	}
	for _, p := range g.players {
		if !p.mario.Dead && p.mario.X+marioW/2 >= g.checkpointX {
			g.checkpointActive = true
			return
		}
	}
}

// checkFlag advances to the next stage when any player reaches the flag, or
// wins the game after the final stage.
func (g *GameActor) checkFlag() {
	reached := false
	for _, p := range g.players {
		if !p.mario.Dead && p.mario.X >= g.flagX-1 {
			reached = true
			break
		}
	}
	if !reached {
		return
	}
	g.score += flagScore + g.timeLeft*10
	if g.stage < len(campaign)-1 {
		g.stageClear = true
		g.transitionTicks = stageClearTicks
		for _, p := range g.players {
			p.mario.VX = 0
			p.mario.VY = 0
		}
		return
	}
	g.won = true
}

// cameraXFor returns the horizontal scroll offset that keeps one player
// roughly centered while clamping to the level bounds.
func (g *GameActor) cameraXFor(p *playerState) float64 {
	cam := p.mario.X - 9
	if cam < 0 {
		cam = 0
	}
	maxCam := float64(LevelW) - 20
	if cam > maxCam {
		cam = maxCam
	}
	return cam
}

// maybeShutdown stops the game when all subscribers have disconnected.
func (g *GameActor) maybeShutdown(ctx *actor.ReceiveContext) {
	if g.hadSubscriber && len(g.players) == 0 {
		ctx.Shutdown()
	}
}

// baseSnapshot builds the shared (non-personalized) part of this tick's
// snapshot. Tiles are attached per subscriber in broadcast.
func (g *GameActor) baseSnapshot() Snapshot {
	def := stageDefinition(g.stage)
	views := make([]PlayerView, len(g.players))
	for i, p := range g.players {
		views[i] = PlayerView{Name: p.name, Mario: p.mario}
	}
	return Snapshot{
		Tick:    g.tickN,
		T:       time.Now().UnixMilli(),
		Players: views,
		// Start from non-nil empty slices: nil marshals to JSON null, which
		// the client's for..of iteration would trip over.
		Enemies:          append([]EnemyState{}, g.enemies...),
		Items:            append([]ItemState{}, g.items...),
		Stage:            g.stage,
		TotalStages:      len(campaign),
		World:            def.World,
		StageInWorld:     def.Stage,
		StageName:        def.Name,
		Theme:            def.Theme,
		StageClear:       g.stageClear,
		Score:            g.score,
		Coins:            g.coins,
		Lives:            g.lives,
		TimeLeft:         g.timeLeft,
		GameOver:         g.gameOver,
		Won:              g.won,
		Paused:           g.paused,
		CheckpointX:      g.checkpointX,
		CheckpointActive: g.checkpointActive,
	}
}

// broadcast sends a personalized Snapshot to every subscribed session. The
// tile grid rides along only when it changed or a subscriber has never seen
// it; everything else is shared by value.
func (g *GameActor) broadcast(ctx *actor.ReceiveContext) {
	if len(g.players) == 0 {
		return
	}
	base := g.baseSnapshot()
	var tilesPayload [][]int8
	for i, p := range g.players {
		snap := base
		snap.You = i
		snap.CameraX = g.cameraXFor(p)
		if g.tilesDirty || p.needsTiles {
			if tilesPayload == nil {
				tilesPayload = g.tilesCopy()
			}
			snap.Tiles = tilesPayload
			p.needsTiles = false
		}
		ctx.Tell(p.pid, &snap)
	}
	g.tilesDirty = false
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
