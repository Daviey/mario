package engine

import "testing"

func TestSecondQuestSwapsGoombasForBuzzies(t *testing.T) {
	classic := newGame(t, DefaultLevels()[0])
	goombas, buzzies := 0, 0
	for _, e := range classic.Enemies {
		if e.Kind == KindGoomba {
			goombas++
		}
	}
	if goombas == 0 {
		t.Fatal("setup: 1-1 has no goombas?")
	}

	g := NewGame(DefaultLevels(), 40, LevelHeight)
	g.beginSecondQuest()
	for g.State != StatePlaying {
		g.Update(Input{})
	}
	for _, e := range g.Enemies {
		switch e.Kind {
		case KindGoomba:
			t.Fatal("a goomba survived the second-quest swap")
		case KindBuzzy:
			buzzies++
		}
	}
	if buzzies != goombas {
		t.Fatalf("swap count: %d buzzies for %d goombas", buzzies, goombas)
	}
}

func TestSecondQuestWalkersQuickened(t *testing.T) {
	if got := (&Game{}).enemySpeed(); got != EnemyWalk {
		t.Fatalf("classic pace = %v, want %v", got, EnemyWalk)
	}
	g := &Game{SecondQuest: true}
	if got := g.enemySpeed(); got != EnemyWalk*QuestSpeed {
		t.Fatalf("quest pace = %v, want %v", got, EnemyWalk*QuestSpeed)
	}
}

func TestWinAnyKeyBeginsSecondQuest(t *testing.T) {
	g := NewGame(DefaultLevels(), 40, LevelHeight)
	g.State = StateWin
	g.Update(Input{})
	g.Update(Input{AnyKey: true})
	if !g.SecondQuest {
		t.Fatal("win-screen any key must arm the second quest")
	}
	if g.State != StateWorldCard || g.LevelIndex() != 0 {
		t.Fatalf("second quest must deal a fresh card at 1-1 (state %v, level %d)", g.State, g.LevelIndex())
	}
}

func TestBeginDailyClearsSecondQuest(t *testing.T) {
	// Daily runs are recorded and ranked — the quest's quickened
	// walkers must never leak into one.
	g := NewGame(DefaultLevels(), 40, LevelHeight)
	g.beginSecondQuest()
	g.BeginDaily()
	if g.SecondQuest {
		t.Fatal("BeginDaily must clear the second-quest flag")
	}
	if g.enemySpeed() != EnemyWalk {
		t.Fatal("daily pace must be the classic pace")
	}
}
