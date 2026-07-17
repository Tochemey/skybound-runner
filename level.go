package main

// Level is a fully-built stage: its tile grid, enemy spawns, the entrance
// spawn point, and the flag column that ends the stage.
type Level struct {
	Tiles   [LevelH][LevelW]int8
	Enemies []EnemyState
	SpawnX  float64
	SpawnY  float64
	FlagX   float64
	Theme   StageTheme
}

// Ground occupies the bottom two rows; surface is the walkable top row.
const (
	groundTopRow = LevelH - 2 // first solid ground row
	surfaceRow   = LevelH - 3 // row Mario stands on top of the ground
)

// newLevel returns a Level with full ground. Campaign metadata places the
// goal flag after each builder completes.
func newLevel() Level {
	var t [LevelH][LevelW]int8
	fillGround(&t, 0, LevelW-1)
	return Level{
		Tiles:  t,
		SpawnX: 2,
		SpawnY: float64(surfaceRow), // settles onto the ground via gravity
		FlagX:  96,
	}
}

// fillGround lays solid ground across [fromX, toX] on the bottom two rows.
func fillGround(t *[LevelH][LevelW]int8, fromX, toX int) {
	for x := fromX; x <= toX; x++ {
		if x < 0 || x >= LevelW {
			continue
		}
		t[groundTopRow][x] = TileGround
		t[LevelH-1][x] = TileGround
	}
}

// carvePit removes ground across [fromX, toX] to create a deadly gap.
func carvePit(t *[LevelH][LevelW]int8, fromX, toX int) {
	for x := fromX; x <= toX; x++ {
		if x < 0 || x >= LevelW {
			continue
		}
		t[groundTopRow][x] = TileAir
		t[LevelH-1][x] = TileAir
	}
}

// fillRect sets a rectangle of tiles to kind, clipped to the level bounds.
func fillRect(t *[LevelH][LevelW]int8, x, y, w, h int, kind int8) {
	for dy := 0; dy < h; dy++ {
		for dx := 0; dx < w; dx++ {
			tx, ty := x+dx, y+dy
			if tx >= 0 && tx < LevelW && ty >= 0 && ty < LevelH {
				t[ty][tx] = kind
			}
		}
	}
}

// placeFlagAt clears an old goal marker and draws a new flag pole at column.
func placeFlagAt(t *[LevelH][LevelW]int8, column int) {
	for y := range t {
		for x := range t[y] {
			if t[y][x] == TileFlag {
				t[y][x] = TileAir
			}
		}
	}
	for y := surfaceRow - 6; y <= surfaceRow; y++ {
		if y >= 0 && column >= 0 && column < LevelW {
			t[y][column] = TileFlag
		}
	}
}

// goomba is a small constructor for a left-walking Goomba on the surface.
func goomba(id, col int) EnemyState {
	return EnemyState{
		ID:    id,
		Kind:  0,
		X:     float64(col),
		Y:     float64(surfaceRow),
		Dir:   -1,
		Alive: true,
	}
}

// goombaAt places a Goomba resting on top of a platform at the given row.
func goombaAt(id, col, topRow int) EnemyState {
	e := goomba(id, col)
	e.Y = float64(topRow - 1)
	return e
}

// stageOne is a gentle introduction. Its platforms stay within the height of
// a normal jump so players can learn movement before the later-stage routes.
func stageOne() Level {
	lv := newLevel()
	t := &lv.Tiles

	fillRect(t, 14, surfaceRow-1, 3, 1, TileBrick)
	t[surfaceRow-1][15] = TileQuestion
	t[surfaceRow-2][20] = TileQuestion

	fillRect(t, 24, surfaceRow-2, 2, 2, TilePipe)
	fillRect(t, 34, surfaceRow-3, 4, 1, TileBrick)
	fillRect(t, 44, surfaceRow-3, 3, 1, TileBrick)

	// Coins floating over the mid-section.
	for _, c := range [][2]int{{35, surfaceRow - 4}, {36, surfaceRow - 4}, {45, surfaceRow - 4}} {
		t[c[1]][c[0]] = TileCoin
	}

	// Staircase up to the flag.
	for i := 0; i < 4; i++ {
		fillRect(t, 82+i, surfaceRow-i, 1, i+1, TileBrick)
	}

	lv.Enemies = []EnemyState{
		goomba(1, 30),
		goomba(2, 40),
		goombaAt(3, 36, surfaceRow-3),
	}
	return lv
}

// stageTwo introduces pits over deadly gaps and taller pipes.
func stageTwo() Level {
	lv := newLevel()
	t := &lv.Tiles

	carvePit(t, 26, 28)
	carvePit(t, 52, 55)
	carvePit(t, 70, 72)

	fillRect(t, 18, surfaceRow-3, 3, 1, TileBrick)
	t[surfaceRow-3][19] = TileQuestion

	fillRect(t, 30, surfaceRow-2, 2, 2, TilePipe)
	fillRect(t, 44, surfaceRow-4, 4, 1, TileBrick)
	fillRect(t, 60, surfaceRow-3, 2, 3, TilePipe)

	// Platform bridge over the wide pit.
	fillRect(t, 52, surfaceRow-3, 4, 1, TileBrick)

	for _, c := range [][2]int{{45, surfaceRow - 5}, {46, surfaceRow - 5}, {53, surfaceRow - 4}, {54, surfaceRow - 4}} {
		t[c[1]][c[0]] = TileCoin
	}

	for i := 0; i < 4; i++ {
		fillRect(t, 82+i, surfaceRow-i, 1, i+1, TileBrick)
	}

	lv.Enemies = []EnemyState{
		goomba(1, 22),
		goomba(2, 40),
		goombaAt(3, 53, surfaceRow-3),
		goomba(4, 64),
		goomba(5, 78),
	}
	return lv
}

// stageThree is the toughest: stepped brick towers, multiple pits, and a
// crowd of Goombas.
func stageThree() Level {
	lv := newLevel()
	t := &lv.Tiles

	carvePit(t, 20, 22)
	carvePit(t, 40, 43)
	carvePit(t, 58, 60)
	carvePit(t, 74, 77)

	// Ascending/descending brick pyramids.
	for i := 0; i < 4; i++ {
		fillRect(t, 28+i, surfaceRow-i, 1, i+1, TileBrick)
	}
	for i := 0; i < 4; i++ {
		fillRect(t, 34+i, surfaceRow-3+i, 1, 4-i, TileBrick)
	}

	fillRect(t, 48, surfaceRow-4, 5, 1, TileBrick)
	t[surfaceRow-4][49] = TileQuestion
	t[surfaceRow-4][51] = TileQuestion

	fillRect(t, 62, surfaceRow-2, 2, 2, TilePipe)
	fillRect(t, 66, surfaceRow-5, 4, 1, TileBrick)

	// Floating coin arc.
	for i := 0; i < 5; i++ {
		t[surfaceRow-6][64+i] = TileCoin
	}

	// Final staircase to the flag.
	for i := 0; i < 5; i++ {
		fillRect(t, 80+i, surfaceRow-i, 1, i+1, TileBrick)
	}

	lv.Enemies = []EnemyState{
		goomba(1, 16),
		goomba(2, 32),
		goombaAt(3, 50, surfaceRow-4),
		goomba(4, 55),
		goomba(5, 68),
		goomba(6, 72),
		goomba(7, 79),
	}
	return lv
}

// placePipe adds a solid pipe rising height tiles from the ground.
func placePipe(t *[LevelH][LevelW]int8, x, height int) {
	fillRect(t, x, groundTopRow-height, 2, height, TilePipe)
}

// placeStairs adds a climbable brick staircase.
func placeStairs(t *[LevelH][LevelW]int8, startX, steps int) {
	for i := 0; i < steps; i++ {
		fillRect(t, startX+i, surfaceRow-i, 1, i+1, TileBrick)
	}
}

// placeCoinArc adds a shallow, collectible arc centered at x.
func placeCoinArc(t *[LevelH][LevelW]int8, x, y, width int) {
	for i := 0; i < width; i++ {
		offset := i - width/2
		arcY := y
		if offset == 0 {
			arcY--
		}
		if x+i >= 0 && x+i < LevelW && arcY >= 0 {
			t[arcY][x+i] = TileCoin
		}
	}
}

// sunnyPath introduces small pits and low stepping stones.
func sunnyPath() Level {
	lv := newLevel()
	t := &lv.Tiles
	carvePit(t, 28, 29)
	carvePit(t, 58, 60)
	fillRect(t, 15, surfaceRow-1, 4, 1, TileBrick)
	t[surfaceRow-1][16] = TileQuestion
	placePipe(t, 35, 2)
	fillRect(t, 43, surfaceRow-2, 5, 1, TileBrick)
	fillRect(t, 58, surfaceRow-2, 3, 1, TileBrick)
	placeCoinArc(t, 43, surfaceRow-4, 5)
	placeStairs(t, 76, 4)
	lv.Enemies = []EnemyState{
		goomba(1, 22), goomba(2, 39), goombaAt(3, 45, surfaceRow-2), goomba(4, 69),
	}
	return lv
}

// sunnyHills finishes the first world with safe, wide elevated routes.
func sunnyHills() Level {
	lv := newLevel()
	t := &lv.Tiles
	carvePit(t, 36, 38)
	fillRect(t, 12, surfaceRow-1, 3, 1, TileBrick)
	fillRect(t, 20, surfaceRow-2, 4, 1, TileBrick)
	t[surfaceRow-2][21] = TileQuestion
	fillRect(t, 30, surfaceRow-3, 5, 1, TileBrick)
	fillRect(t, 38, surfaceRow-2, 4, 1, TileBrick)
	placePipe(t, 50, 3)
	fillRect(t, 59, surfaceRow-2, 5, 1, TileBrick)
	placeCoinArc(t, 59, surfaceRow-4, 5)
	placeStairs(t, 78, 5)
	lv.Enemies = []EnemyState{
		goomba(1, 17), goombaAt(2, 32, surfaceRow-3), goomba(3, 48), goomba(4, 67), goomba(5, 74),
	}
	return lv
}

// pipeVault uses tall conduits and bridge platforms over wider gaps.
func pipeVault() Level {
	lv := newLevel()
	t := &lv.Tiles
	carvePit(t, 24, 27)
	carvePit(t, 54, 57)
	placePipe(t, 15, 2)
	placePipe(t, 32, 3)
	fillRect(t, 24, surfaceRow-2, 4, 1, TileBrick)
	fillRect(t, 41, surfaceRow-3, 5, 1, TileBrick)
	t[surfaceRow-3][43] = TileQuestion
	fillRect(t, 54, surfaceRow-3, 4, 1, TileBrick)
	placePipe(t, 64, 4)
	fillRect(t, 72, surfaceRow-2, 5, 1, TileBrick)
	placeStairs(t, 82, 4)
	placeCoinArc(t, 41, surfaceRow-5, 5)
	lv.Enemies = []EnemyState{
		goomba(1, 20), goombaAt(2, 25, surfaceRow-2), goombaAt(3, 43, surfaceRow-3), goomba(4, 61), goomba(5, 78),
	}
	return lv
}

// pipeRun layers short bridges and patrols for an industrial finale.
func pipeRun() Level {
	lv := newLevel()
	t := &lv.Tiles
	carvePit(t, 20, 22)
	carvePit(t, 45, 48)
	carvePit(t, 70, 72)
	placePipe(t, 12, 2)
	fillRect(t, 20, surfaceRow-2, 3, 1, TileBrick)
	fillRect(t, 29, surfaceRow-3, 5, 1, TileBrick)
	placePipe(t, 38, 4)
	fillRect(t, 45, surfaceRow-3, 4, 1, TileBrick)
	fillRect(t, 55, surfaceRow-4, 5, 1, TileBrick)
	placePipe(t, 63, 3)
	fillRect(t, 70, surfaceRow-2, 4, 1, TileBrick)
	placeStairs(t, 82, 5)
	placeCoinArc(t, 55, surfaceRow-6, 5)
	lv.Enemies = []EnemyState{
		goomba(1, 17), goombaAt(2, 30, surfaceRow-3), goomba(3, 42), goombaAt(4, 56, surfaceRow-4), goomba(5, 67), goomba(6, 77),
	}
	return lv
}

// emberSteps introduces the dense staircase routes of the ruins.
func emberSteps() Level {
	lv := newLevel()
	t := &lv.Tiles
	carvePit(t, 25, 27)
	carvePit(t, 57, 60)
	placeStairs(t, 14, 4)
	fillRect(t, 30, surfaceRow-3, 5, 1, TileBrick)
	t[surfaceRow-3][32] = TileQuestion
	placeStairs(t, 39, 5)
	fillRect(t, 57, surfaceRow-3, 4, 1, TileBrick)
	fillRect(t, 67, surfaceRow-4, 5, 1, TileBrick)
	placeStairs(t, 80, 5)
	placeCoinArc(t, 67, surfaceRow-6, 5)
	lv.Enemies = []EnemyState{
		goomba(1, 20), goombaAt(2, 31, surfaceRow-3), goomba(3, 37), goomba(4, 53), goombaAt(5, 69, surfaceRow-4), goomba(6, 76),
	}
	return lv
}

// emberSpan mixes long gaps and bridge platforms in the late campaign.
func emberSpan() Level {
	lv := newLevel()
	t := &lv.Tiles
	carvePit(t, 18, 21)
	carvePit(t, 42, 46)
	carvePit(t, 68, 71)
	fillRect(t, 18, surfaceRow-2, 4, 1, TileBrick)
	fillRect(t, 28, surfaceRow-4, 5, 1, TileBrick)
	placePipe(t, 36, 4)
	fillRect(t, 42, surfaceRow-3, 5, 1, TileBrick)
	fillRect(t, 54, surfaceRow-4, 5, 1, TileBrick)
	fillRect(t, 68, surfaceRow-2, 4, 1, TileBrick)
	placeStairs(t, 80, 5)
	placeCoinArc(t, 54, surfaceRow-6, 5)
	lv.Enemies = []EnemyState{
		goomba(1, 15), goombaAt(2, 29, surfaceRow-4), goomba(3, 35), goombaAt(4, 44, surfaceRow-3), goombaAt(5, 55, surfaceRow-4), goomba(6, 75),
	}
	return lv
}

// emberCrown is the final stage, combining every established obstacle type.
func emberCrown() Level {
	lv := newLevel()
	t := &lv.Tiles
	carvePit(t, 16, 18)
	carvePit(t, 37, 40)
	carvePit(t, 62, 65)
	carvePit(t, 74, 77)
	placeStairs(t, 22, 4)
	fillRect(t, 32, surfaceRow-4, 4, 1, TileBrick)
	fillRect(t, 37, surfaceRow-3, 4, 1, TileBrick)
	placePipe(t, 48, 4)
	fillRect(t, 56, surfaceRow-5, 5, 1, TileBrick)
	fillRect(t, 62, surfaceRow-3, 4, 1, TileBrick)
	fillRect(t, 70, surfaceRow-4, 4, 1, TileBrick)
	fillRect(t, 74, surfaceRow-2, 4, 1, TileBrick)
	placeStairs(t, 82, 6)
	placeCoinArc(t, 56, surfaceRow-7, 5)
	lv.Enemies = []EnemyState{
		goomba(1, 13), goombaAt(2, 24, surfaceRow-3), goombaAt(3, 33, surfaceRow-4), goomba(4, 45),
		goombaAt(5, 57, surfaceRow-5), goomba(6, 68), goombaAt(7, 71, surfaceRow-4), goomba(8, 79),
	}
	return lv
}
