package main

import "testing"

func TestCampaignHasThreeWorldsAndTenStages(t *testing.T) {
	if got, want := len(campaign), 10; got != want {
		t.Fatalf("campaign has %d stages, want %d", got, want)
	}

	worldCounts := map[int]int{}
	for index, def := range campaign {
		worldCounts[def.World]++
		if def.Stage < 1 || def.Name == "" || def.Build == nil {
			t.Fatalf("campaign stage %d has incomplete metadata: %#v", index, def)
		}

		level, builtDef := buildStage(index)
		if builtDef.World != def.World || builtDef.Stage != def.Stage || builtDef.Name != def.Name {
			t.Fatalf("buildStage(%d) returned unexpected metadata", index)
		}
		if level.FlagX != float64(def.FlagCol) {
			t.Fatalf("stage %d flag at %.0f, want %d", index, level.FlagX, def.FlagCol)
		}
		if level.Tiles[surfaceRow][def.FlagCol] != TileFlag {
			t.Fatalf("stage %d has no flag at column %d", index, def.FlagCol)
		}
		if level.Tiles[groundTopRow][int(level.SpawnX)] != TileGround {
			t.Fatalf("stage %d spawn has no supporting ground", index)
		}
	}

	if worldCounts[1] != 3 || worldCounts[2] != 3 || worldCounts[3] != 4 {
		t.Fatalf("world distribution = %#v, want 3/3/4", worldCounts)
	}
}

func TestQuestionBlockAwardsCoin(t *testing.T) {
	level, _ := buildStage(0)
	game := &GameActor{
		tiles: level.Tiles,
		mario: MarioState{X: 15, Y: float64(surfaceRow), Facing: 1},
	}

	game.hitBlockFromBelow(surfaceRow - 1)

	if got, want := game.coins, 1; got != want {
		t.Fatalf("coins = %d, want %d", got, want)
	}
	if got, want := game.score, coinScore; got != want {
		t.Fatalf("score = %d, want %d", got, want)
	}
	if got := game.tiles[surfaceRow-1][15]; got != TileBrick {
		t.Fatalf("question block = %d, want TileBrick", got)
	}
}

func TestFlagStartsStageClearTransition(t *testing.T) {
	level, _ := buildStage(0)
	game := &GameActor{
		stage:    0,
		tiles:    level.Tiles,
		mario:    MarioState{X: level.FlagX - 1, Y: level.SpawnY},
		flagX:    level.FlagX,
		timeLeft: 100,
	}

	game.checkFlag()

	if !game.stageClear {
		t.Fatal("flag did not start stage-clear transition")
	}
	if got, want := game.transitionTicks, stageClearTicks; got != want {
		t.Fatalf("transition ticks = %d, want %d", got, want)
	}
}
