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

import "testing"

// newTestGame builds a GameActor on the given stage with one player, without
// an actor system. Physics methods are all callable directly.
func newTestGame(t *testing.T, stage int) (*GameActor, *playerState) {
	t.Helper()
	g := &GameActor{}
	g.reset()
	for g.stage != stage {
		g.stage++
		g.loadStage()
	}
	g.addPlayer("p1", nil)
	return g, g.players[0]
}

// settle steps the game until the player lands on the ground.
func settle(t *testing.T, g *GameActor, p *playerState) {
	t.Helper()
	for i := 0; i < 120 && !p.mario.OnGround; i++ {
		g.step()
	}
	if !p.mario.OnGround {
		t.Fatal("player never landed on the ground")
	}
}

func TestJumpPressLaunchesFromGround(t *testing.T) {
	g, p := newTestGame(t, 0)
	settle(t, g, p)

	g.handleInput(p, ActionJump)
	g.step()

	if p.mario.VY >= 0 {
		t.Fatalf("VY = %.3f after jump, want negative (moving up)", p.mario.VY)
	}
	if p.mario.OnGround {
		t.Fatal("player still grounded after jump")
	}
}

func TestJumpBufferFiresOnLanding(t *testing.T) {
	g, p := newTestGame(t, 0)
	settle(t, g, p)

	// Airborne after a jump; press again mid-air just before landing. The
	// buffered press must fire on touchdown, and never double-jump mid-air.
	g.handleInput(p, ActionJump)
	g.step()
	for i := 0; i < 60 && p.mario.VY < 0; i++ {
		g.step() // ride to the apex
	}
	risingVY := p.mario.VY
	g.handleInput(p, ActionJump) // buffered press while falling
	g.step()
	if p.mario.VY < risingVY {
		t.Fatal("jump fired while airborne — double jump must not exist")
	}
}

func TestCoinRollsOverIntoExtraLife(t *testing.T) {
	g, _ := newTestGame(t, 0)
	g.coins = coinsPerLife - 1
	livesBefore := g.lives

	g.addCoin()

	if g.lives != livesBefore+1 {
		t.Fatalf("lives = %d, want %d", g.lives, livesBefore+1)
	}
	if g.coins != 0 {
		t.Fatalf("coins = %d after rollover, want 0", g.coins)
	}
}

func TestQuestionBlockAwardsCoin(t *testing.T) {
	g, p := newTestGame(t, 0)
	// Block at (20, surfaceRow-2) hashes to a coin, not a shield.
	if blockHoldsShield(20, surfaceRow-2) {
		t.Fatal("test premise broken: block (20) should hold a coin")
	}
	p.mario.X = 20
	g.hitBlockFromBelow(p, surfaceRow-2)

	if got, want := g.coins, 1; got != want {
		t.Fatalf("coins = %d, want %d", got, want)
	}
	if got, want := g.score, coinScore; got != want {
		t.Fatalf("score = %d, want %d", got, want)
	}
	if got := g.tiles[surfaceRow-2][20]; got != TileBrick {
		t.Fatalf("question block = %d, want TileBrick", got)
	}
	if !g.tilesDirty {
		t.Fatal("hitting a block must mark tiles dirty for the delta protocol")
	}
}

func TestQuestionBlockReleasesShield(t *testing.T) {
	g, p := newTestGame(t, 0)
	// Block at (15, surfaceRow-1) hashes to a shield.
	if !blockHoldsShield(15, surfaceRow-1) {
		t.Fatal("test premise broken: block (15) should hold a shield")
	}
	p.mario.X = 15
	g.hitBlockFromBelow(p, surfaceRow-1)

	if len(g.items) != 1 || !g.items[0].Alive || g.items[0].Kind != ItemShield {
		t.Fatalf("items = %#v, want one live shield", g.items)
	}
	if g.coins != 0 {
		t.Fatalf("coins = %d, want 0 (block held a shield)", g.coins)
	}
}

func TestShieldAbsorbsOneHit(t *testing.T) {
	g, p := newTestGame(t, 0)
	p.mario.Shield = true

	g.killPlayer(p, false)

	if p.mario.Dead {
		t.Fatal("shielded player died from a single hit")
	}
	if p.mario.Shield {
		t.Fatal("shield survived the hit it absorbed")
	}
	if p.mario.Invuln == 0 {
		t.Fatal("no invulnerability after shield break")
	}

	// A second hit during invulnerability is also ignored.
	g.killPlayer(p, false)
	if p.mario.Dead {
		t.Fatal("player died while invulnerable")
	}

	// Pits are always fatal, shield or not.
	p.mario.Invuln = 0
	p.mario.Shield = true
	g.killPlayer(p, true)
	if !p.mario.Dead {
		t.Fatal("force kill must bypass the shield")
	}
}

func TestDeathTumbleConsumesLifeAndRespawns(t *testing.T) {
	g, p := newTestGame(t, 0)
	settle(t, g, p)
	livesBefore := g.lives

	g.killPlayer(p, true)
	if !p.mario.Dead || p.deathTicks != deathAnimTicks {
		t.Fatal("death did not start a tumble animation")
	}
	for i := 0; i < deathAnimTicks+1; i++ {
		g.step()
	}

	if p.mario.Dead {
		t.Fatal("player not respawned after the tumble")
	}
	if g.lives != livesBefore-1 {
		t.Fatalf("lives = %d, want %d", g.lives, livesBefore-1)
	}
	if p.mario.X != g.spawnX {
		t.Fatalf("respawned at %.1f, want spawn %.1f", p.mario.X, g.spawnX)
	}
}

func TestGameOverWhenLivesRunOut(t *testing.T) {
	g, p := newTestGame(t, 0)
	settle(t, g, p)
	g.lives = 1

	g.killPlayer(p, true)
	for i := 0; i < deathAnimTicks+1 && !g.gameOver; i++ {
		g.step()
	}

	if !g.gameOver {
		t.Fatal("game did not end when the last life was lost")
	}
}

func TestStompSquashesCrawler(t *testing.T) {
	g, p := newTestGame(t, 0)
	g.enemies = []EnemyState{{ID: 1, Kind: EnemyCrawler, X: 10, Y: float64(surfaceRow), Dir: -1, Alive: true}}
	// Falling onto the enemy's head from above.
	p.mario.X = 10
	p.mario.Y = float64(surfaceRow) - 0.9
	p.mario.VY = 0.4
	p.prevBottom = p.mario.Y + marioH - 0.4

	g.checkEnemyCollisions(p)

	e := &g.enemies[0]
	if e.Alive {
		t.Fatal("stomped crawler still alive")
	}
	if e.Dying != squashTicks {
		t.Fatalf("Dying = %d, want %d (squash linger for the client)", e.Dying, squashTicks)
	}
	if p.mario.VY >= 0 {
		t.Fatal("no stomp bounce")
	}
	if p.mario.Dead {
		t.Fatal("player died from a valid stomp")
	}
}

func TestSpikyCannotBeStomped(t *testing.T) {
	g, p := newTestGame(t, 0)
	g.enemies = []EnemyState{{ID: 1, Kind: EnemySpiky, X: 10, Y: float64(surfaceRow), Dir: -1, Alive: true}}
	p.mario.X = 10
	p.mario.Y = float64(surfaceRow) - 0.9
	p.mario.VY = 0.4
	p.prevBottom = p.mario.Y + marioH - 0.4

	g.checkEnemyCollisions(p)

	if g.enemies[0].Dying > 0 || !g.enemies[0].Alive {
		t.Fatal("spiky was stomped — it must never be")
	}
	if !p.mario.Dead {
		t.Fatal("landing on a spiky must hurt the player")
	}
}

func TestCrouchShrinksHitboxAndDucksUnderFlyer(t *testing.T) {
	g, p := newTestGame(t, 0)
	settle(t, g, p)

	g.handleInput(p, ActionDown)
	g.step()
	if !p.mario.Crouch {
		t.Fatal("holding Down on the ground did not crouch")
	}
	if p.height() != crouchH {
		t.Fatalf("crouch height = %.2f, want %.2f", p.height(), crouchH)
	}

	// A flyer grazing standing head height misses a crouched player: its box
	// bottom (Y+0.8 = surfaceRow+0.3) sits below the standing head top
	// (surfaceRow+0.02) but above the crouched head top (surfaceRow+0.4).
	flyerY := float64(surfaceRow) - 0.5
	g.enemies = []EnemyState{{ID: 9, Kind: EnemyFlyer, X: p.mario.X, Y: flyerY, BaseX: p.mario.X, BaseY: flyerY, Dir: 1, Alive: true}}
	g.checkEnemyCollisions(p)
	if p.mario.Dead {
		t.Fatal("crouched player was hit by a flyer above their head")
	}

	// Stand up via the crouch logic directly (step would also advance the
	// flyer's sine bob, making the geometry nondeterministic).
	g.handleInput(p, ActionDownEnd)
	g.applyCrouch(p)
	if p.mario.Crouch {
		t.Fatal("player did not stand back up in open space")
	}
	g.checkEnemyCollisions(p)
	if !p.mario.Dead {
		t.Fatal("standing player should be clipped by the same flyer")
	}
}

func TestCheckpointActivatesAndRespawns(t *testing.T) {
	g, p := newTestGame(t, 0)
	settle(t, g, p)
	if g.checkpointX < 0 {
		t.Fatal("stage one placed no checkpoint")
	}
	if g.tiles[surfaceRow][int(g.checkpointX)] != TileCheckpoint {
		t.Fatal("checkpoint tile missing from the grid")
	}

	p.mario.X = g.checkpointX + 1
	g.checkCheckpoint()
	if !g.checkpointActive {
		t.Fatal("crossing the checkpoint did not activate it")
	}

	g.killPlayer(p, true)
	for i := 0; i < deathAnimTicks+1; i++ {
		g.step()
	}
	if p.mario.X != g.checkpointX {
		t.Fatalf("respawned at %.1f, want checkpoint %.1f", p.mario.X, g.checkpointX)
	}
}

func TestFlyerStaysWithinPatrolBounds(t *testing.T) {
	g, _ := newTestGame(t, 3) // stage four (W2-1) has a flyer
	var fl *EnemyState
	for i := range g.enemies {
		if g.enemies[i].Kind == EnemyFlyer {
			fl = &g.enemies[i]
			break
		}
	}
	if fl == nil {
		t.Fatal("stage four has no flyer")
	}
	for i := 0; i < 1200; i++ {
		g.updateEnemies()
		if fl.X < fl.BaseX-flyerRange-1e-6 || fl.X > fl.BaseX+flyerRange+1e-6 {
			t.Fatalf("flyer drifted to X=%.2f, patrol anchor %.2f ± %.1f", fl.X, fl.BaseX, flyerRange)
		}
		if fl.Y < fl.BaseY-flyerBob-1e-6 || fl.Y > fl.BaseY+flyerBob+1e-6 {
			t.Fatalf("flyer drifted to Y=%.2f, anchor %.2f ± %.1f", fl.Y, fl.BaseY, flyerBob)
		}
	}
}

func TestFlagStartsStageClearTransition(t *testing.T) {
	g, p := newTestGame(t, 0)
	settle(t, g, p)
	p.mario.X = g.flagX - 1

	g.checkFlag()

	if !g.stageClear {
		t.Fatal("flag did not start stage-clear transition")
	}
	if got, want := g.transitionTicks, stageClearTicks; got != want {
		t.Fatalf("transition ticks = %d, want %d", got, want)
	}
}

func TestFinalFlagWinsGame(t *testing.T) {
	g, p := newTestGame(t, len(campaign)-1)
	settle(t, g, p)
	p.mario.X = g.flagX

	g.checkFlag()

	if !g.won {
		t.Fatal("reaching the final flag did not win the game")
	}
}

func TestSnapshotTilesAreDelta(t *testing.T) {
	g, p := newTestGame(t, 0)
	if !g.tilesDirty {
		t.Fatal("fresh stage must mark tiles dirty")
	}
	if !p.needsTiles {
		t.Fatal("new subscriber must be owed a full tile grid")
	}
	if g.baseSnapshot().Tiles != nil {
		t.Fatal("base snapshot must not carry tiles — they attach per subscriber")
	}

	g.tilesDirty = false
	// Collect a coin: the grid changed, so the next snapshot must carry tiles.
	p.mario.X, p.mario.Y = 35, float64(surfaceRow-4)
	g.collectCoins(p)
	if !g.tilesDirty {
		t.Fatal("collecting a coin did not mark tiles dirty")
	}
}

func TestShieldItemCollectableAfterSliding(t *testing.T) {
	g, p := newTestGame(t, 0)
	settle(t, g, p)
	g.spawnItem(ItemShield, p.mario.X+2, p.mario.Y-2)
	g.items[0].Dir = -1 // slide toward the player

	collected := false
	for i := 0; i < 300; i++ {
		g.step()
		if p.mario.Shield {
			collected = true
			break
		}
	}
	if !collected {
		t.Fatal("sliding shield item was never collected by an adjacent player")
	}
	if g.score < shieldScore {
		t.Fatalf("score = %d, want at least %d for the shield pickup", g.score, shieldScore)
	}
}
