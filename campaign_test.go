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

		// Enemy IDs must be unique within a stage (the client lerps by ID),
		// and kinds must be ones both sides understand.
		seen := map[int]bool{}
		for _, e := range level.Enemies {
			if seen[e.ID] {
				t.Fatalf("stage %d has duplicate enemy id %d", index, e.ID)
			}
			seen[e.ID] = true
			if e.Kind < EnemyCrawler || e.Kind > EnemySpiky {
				t.Fatalf("stage %d enemy %d has unknown kind %d", index, e.ID, e.Kind)
			}
			if e.Kind == EnemyFlyer && (e.BaseX != e.X || e.BaseY != e.Y) {
				t.Fatalf("stage %d flyer %d anchor not at spawn", index, e.ID)
			}
		}
	}

	if worldCounts[1] != 3 || worldCounts[2] != 3 || worldCounts[3] != 4 {
		t.Fatalf("world distribution = %#v, want 3/3/4", worldCounts)
	}
}

func TestLaterWorldsIncludeNewEnemyKinds(t *testing.T) {
	flyers, spikies := 0, 0
	for index := range campaign {
		level, _ := buildStage(index)
		for _, e := range level.Enemies {
			switch e.Kind {
			case EnemyFlyer:
				flyers++
			case EnemySpiky:
				spikies++
			}
		}
	}
	if flyers == 0 || spikies == 0 {
		t.Fatalf("campaign has %d flyers and %d spikies, want both > 0", flyers, spikies)
	}
}
