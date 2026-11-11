package fsrs_test

import (
	"math"
	"testing"
	"time"

	"github.com/reithan/teach-me/internal/fsrs"
)

var t0 = time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)

// review applies one rating to a card and returns the card as the next review
// will see it, plus the result.
func review(c fsrs.Card, g int, now time.Time) (fsrs.Card, fsrs.Result) {
	res := fsrs.Next(c, g, now, fsrs.DefaultRetention)
	lapsed := c.Lapsed || (c.Reviewed && c.Step == 0 && g == fsrs.Again)
	return fsrs.Card{Reviewed: true, S: res.S, D: res.D, Step: res.Step, Lapsed: lapsed, Last: now}, res
}

func near(a, b float64) bool { return math.Abs(a-b) < 0.00006 }

// TestWorkedValues checks the table of spec §17.4.
func TestWorkedValues(t *testing.T) {
	day := 24 * time.Hour
	later := t0.Add(2 * day)
	type step struct {
		g   int
		now time.Time
	}
	cases := []struct {
		name    string
		steps   []step
		s, d    float64
		due     time.Duration // from the last review
		wantLft int
	}{
		{"good", []step{{3, t0}}, 2.3065, 2.1181, 10 * time.Minute, 1},
		{"easy", []step{{4, t0}}, 8.2956, 1.0000, 8 * day, 0},
		{"again", []step{{1, t0}}, 0.2120, 6.4133, time.Minute, 2},
		{"good good same day", []step{{3, t0}, {3, t0.Add(10 * time.Minute)}}, 2.3065, 2.1112, 2 * day, 0},
		{"good again same day", []step{{3, t0}, {1, t0.Add(10 * time.Minute)}}, 0.7751, 7.3945, time.Minute, 2},
		{"good good, good 2 days later", []step{{3, t0}, {3, t0}, {3, later}}, 10.9710, 2.1043, 11 * day, 0},
		{"good good, hard 2 days later", []step{{3, t0}, {3, t0}, {2, later}}, 7.5174, 4.7483, 8 * day, 0},
		{"good good, easy 2 days later", []step{{3, t0}, {3, t0}, {4, later}}, 18.5343, 1.0000, 19 * day, 0},
		{"good good, again 2 days later", []step{{3, t0}, {3, t0}, {1, later}}, 0.6077, 7.3922, 10 * time.Minute, 1},
	}
	for _, tc := range cases {
		var c fsrs.Card
		var res fsrs.Result
		var last time.Time
		for _, st := range tc.steps {
			c, res = review(c, st.g, st.now)
			last = st.now
		}
		if !near(res.S, tc.s) || !near(res.D, tc.d) {
			t.Errorf("%s: S %.4f D %.4f, want %.4f %.4f", tc.name, res.S, res.D, tc.s, tc.d)
		}
		if got := res.Due.Sub(last); got != tc.due {
			t.Errorf("%s: due in %v, want %v", tc.name, got, tc.due)
		}
		if res.Step != tc.wantLft {
			t.Errorf("%s: steps left %d, want %d", tc.name, res.Step, tc.wantLft)
		}
		if res.Due.Location() != time.UTC {
			t.Errorf("%s: due is not UTC", tc.name)
		}
	}
}

func TestRecallAtTwoDays(t *testing.T) {
	if r := fsrs.Recall(2, 2.3065); math.Abs(r-0.9095) > 0.00006 {
		t.Errorf("recall %.4f, want 0.9095", r)
	}
	if r := fsrs.Recall(10, 10); math.Abs(r-0.9) > 1e-9 {
		t.Errorf("recall at t = S is %.6f, want 0.9", r)
	}
}

func TestIntervalByRetention(t *testing.T) {
	for r, want := range map[float64]int{0.9: 10, 0.95: 4, 0.8: 33} {
		if got := fsrs.Interval(10, r); got != want {
			t.Errorf("retention %.2f: interval %d, want %d", r, got, want)
		}
	}
	if got := fsrs.Interval(0.001, 0.9); got != 1 {
		t.Errorf("smallest interval %d, want 1", got)
	}
	if got := fsrs.Interval(36500, 0.7); got != 36500 {
		t.Errorf("largest interval %d, want 36500", got)
	}
}

func TestSteps(t *testing.T) {
	var fresh fsrs.Card

	// Hard on a learning step stays on it, due in 6 minutes.
	c, res := review(fresh, fsrs.Hard, t0)
	if res.Step != 2 || res.Due.Sub(t0) != 6*time.Minute {
		t.Errorf("new hard: step %d due %v", res.Step, res.Due.Sub(t0))
	}
	c, res = review(c, fsrs.Good, t0)
	if res.Step != 1 || res.Due.Sub(t0) != 10*time.Minute {
		t.Errorf("then good: step %d due %v", res.Step, res.Due.Sub(t0))
	}
	_, res = review(c, fsrs.Hard, t0)
	if res.Step != 1 || res.Due.Sub(t0) != 6*time.Minute {
		t.Errorf("hard on the second step: step %d due %v", res.Step, res.Due.Sub(t0))
	}
	_, res = review(c, fsrs.Easy, t0)
	if res.Step != 0 || res.Due.Sub(t0) < 24*time.Hour {
		t.Errorf("easy on a step should reach review: step %d due %v", res.Step, res.Due.Sub(t0))
	}

	// Relearning: one 10-minute step; hard is 15 minutes; again stays.
	c, _ = review(fresh, fsrs.Easy, t0)
	lapse := t0.Add(8 * 24 * time.Hour)
	c, res = review(c, fsrs.Again, lapse)
	if res.Step != 1 || res.Due.Sub(lapse) != 10*time.Minute || !c.Lapsed {
		t.Errorf("lapse: step %d due %v lapsed %v", res.Step, res.Due.Sub(lapse), c.Lapsed)
	}
	_, res = review(c, fsrs.Hard, lapse)
	if res.Step != 1 || res.Due.Sub(lapse) != 15*time.Minute {
		t.Errorf("relearning hard: step %d due %v", res.Step, res.Due.Sub(lapse))
	}
	_, res = review(c, fsrs.Again, lapse)
	if res.Step != 1 || res.Due.Sub(lapse) != 10*time.Minute {
		t.Errorf("relearning again: step %d due %v", res.Step, res.Due.Sub(lapse))
	}
	_, res = review(c, fsrs.Good, lapse)
	if res.Step != 0 || res.Due.Sub(lapse) < 24*time.Hour {
		t.Errorf("relearning good should return to review: step %d due %v", res.Step, res.Due.Sub(lapse))
	}
}

func TestReviewIntervalsAreOrdered(t *testing.T) {
	// A card with a tiny stability would get 1 day for every rating; the
	// ordering spreads them to 1, 2, 3.
	c := fsrs.Card{Reviewed: true, S: 0.05, D: 9, Last: t0.Add(-24 * time.Hour)}
	want := map[int]int{fsrs.Hard: 1, fsrs.Good: 2, fsrs.Easy: 3}
	for g, days := range want {
		res := fsrs.Next(c, g, t0, fsrs.DefaultRetention)
		if got := fsrs.Days(t0, res.Due); got != days || res.Step != 0 {
			t.Errorf("rating %d: %d days step %d, want %d days", g, got, res.Step, days)
		}
	}
}

func TestSameDayReviewOfAReviewCardNeverShrinksOnRecall(t *testing.T) {
	c := fsrs.Card{Reviewed: true, S: 50, D: 5, Last: t0}
	for _, g := range []int{fsrs.Hard, fsrs.Good, fsrs.Easy} {
		if res := fsrs.Next(c, g, t0.Add(time.Hour), fsrs.DefaultRetention); res.S < 50 {
			t.Errorf("rating %d shrank S to %.4f", g, res.S)
		}
	}
	if res := fsrs.Next(c, fsrs.Again, t0.Add(time.Hour), fsrs.DefaultRetention); res.S >= 50 || res.Step != 1 {
		t.Errorf("again the same day: S %.4f step %d", res.S, res.Step)
	}
}

func TestBoundsHold(t *testing.T) {
	c := fsrs.Card{Reviewed: true, S: 36500, D: 1, Last: t0.AddDate(-50, 0, 0)}
	res := fsrs.Next(c, fsrs.Easy, t0, fsrs.MinRetention)
	if res.S > 36500 || res.D < 1 || fsrs.Days(t0, res.Due) > 36500 {
		t.Errorf("upper bounds broken: %+v", res)
	}
	c = fsrs.Card{Reviewed: true, S: 0.001, D: 10, Last: t0.AddDate(0, 0, -400)}
	res = fsrs.Next(c, fsrs.Again, t0, fsrs.DefaultRetention)
	if res.S < 0.001 || res.D > 10 {
		t.Errorf("lower bounds broken: %+v", res)
	}
}

func TestDaysUsesTheLocalDate(t *testing.T) {
	zone := time.FixedZone("west", -8*3600)
	evening := time.Date(2026, 11, 11, 23, 30, 0, 0, zone) // 07:30 UTC on the 12th
	morning := time.Date(2026, 11, 12, 0, 30, 0, 0, zone)
	if got := fsrs.Days(evening.UTC(), morning); got != 1 {
		t.Errorf("one hour across local midnight: %d days, want 1", got)
	}
	if got := fsrs.Days(evening.UTC(), morning.UTC()); got != 0 {
		t.Errorf("the same two times read in UTC: %d days, want 0", got)
	}
	if got := fsrs.Days(morning, evening); got != 0 {
		t.Errorf("a review dated after now: %d days, want 0", got)
	}
}

func TestValid(t *testing.T) {
	for g, want := range map[int]bool{0: false, 1: true, 4: true, 5: false} {
		if fsrs.ValidRating(g) != want {
			t.Errorf("ValidRating(%d) != %v", g, want)
		}
	}
	for r, want := range map[float64]bool{0.69: false, 0.70: true, 0.9: true, 0.97: true, 0.98: false} {
		if fsrs.ValidRetention(r) != want {
			t.Errorf("ValidRetention(%v) != %v", r, want)
		}
	}
}
