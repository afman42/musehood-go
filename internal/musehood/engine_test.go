package musehood

import "testing"

func TestRouteMentionPatternCacheRefreshesAfterSeatRename(t *testing.T) {
	a := &App{data: data{
		Seats: []seat{
			{identity: identity{Name: "Muse"}, Code: "MUSE"},
			{identity: identity{Name: "Opus"}, Code: "M10"},
		},
		Settings: settings{RoundRobin: false},
	}}

	if _, steps := a.routeLocked("@Opus hello", nil); len(steps) != 1 || steps[0].Seat != "M10" {
		t.Fatalf("initial mention routed incorrectly: %+v", steps)
	}
	a.data.Seats[1].Name = "Maestro"
	if _, steps := a.routeLocked("@Maestro hello", nil); len(steps) != 1 || steps[0].Seat != "M10" {
		t.Fatalf("renamed mention routed incorrectly: %+v", steps)
	}
	if _, steps := a.routeLocked("@Opus hello", nil); len(steps) != 1 || steps[0].Seat != "MUSE" {
		t.Fatalf("stale mention pattern was retained: %+v", steps)
	}
}
