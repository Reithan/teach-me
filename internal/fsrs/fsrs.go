// Package fsrs is the flashcard scheduler of spec §17.4: FSRS-6 (Free Spaced
// Repetition Scheduler, version 6) with its published default parameters, and
// the learning and relearning steps around it. It is pure arithmetic over a
// card's last review; it reads no files and adds no random spread, so the same
// review history always gives the same dates.
package fsrs

import (
	"math"
	"time"
)

// Ratings.
const (
	Again = 1
	Hard  = 2
	Good  = 3
	Easy  = 4
)

// Retention bounds and default (§17.4, TM_CARD_RETENTION).
const (
	DefaultRetention = 0.9
	MinRetention     = 0.70
	MaxRetention     = 0.97
)

// LearningSteps is the number of steps a new card passes through before it
// gets an interval in days. A lapsed card passes through one relearning step.
const LearningSteps = 2

const (
	minStability  = 0.001
	maxStability  = 36500
	firstFloor    = 0.1
	maxInterval   = 36500
	firstStep     = 1 * time.Minute
	secondStep    = 10 * time.Minute
	relearnStep   = 10 * time.Minute
	hardLearning  = 6 * time.Minute
	hardRelearn   = 15 * time.Minute
	hoursInOneDay = 24 * time.Hour
)

// w holds the FSRS-6 default parameters w0 to w20.
var w = [21]float64{
	0.212, 1.2931, 2.3065, 8.2956, 6.4133, 0.8334, 3.0194,
	0.001, 1.8722, 0.1666, 0.796, 1.4835, 0.0614, 0.2629,
	1.6483, 0.6014, 1.8729, 0.5425, 0.0912, 0.0658, 0.1542,
}

var (
	decay  = -w[20]
	factor = math.Pow(0.9, 1/decay) - 1
)

// Card is what the scheduler needs to know about a card before a review. The
// zero value is a card that has never been reviewed.
type Card struct {
	Reviewed bool      // false for a new card; the other fields are then unused
	S        float64   // stability in days after the last review
	D        float64   // difficulty after the last review, 1 to 10
	Step     int       // steps left after the last review; 0 for a card in review
	Lapsed   bool      // the card has been forgotten in review at least once
	Last     time.Time // time of the last review
}

// Result is the outcome of one review: the values a review row records.
type Result struct {
	S    float64
	D    float64
	Step int // steps left; 0 when the card is in review
	Due  time.Time
}

// ValidRating reports whether g is one of the four ratings.
func ValidRating(g int) bool { return g >= Again && g <= Easy }

// ValidRetention reports whether r is inside the allowed range.
func ValidRetention(r float64) bool { return r >= MinRetention && r <= MaxRetention }

// Recall is the chance of recalling a card of stability s after t days.
func Recall(t, s float64) float64 {
	return math.Pow(1+factor*t/s, decay)
}

// Interval is the number of whole days after which a card of stability s
// falls to the target retention, held to [1, 36500].
func Interval(s, retention float64) int {
	days := math.Round(s / factor * (math.Pow(retention, 1/decay) - 1))
	return int(clamp(days, 1, maxInterval))
}

// Days is the number of calendar days from a to b in b's time zone (§17.4,
// "Days"). It is never negative.
func Days(a, b time.Time) int {
	a = a.In(b.Location())
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	from := time.Date(ay, am, ad, 0, 0, 0, 0, time.UTC)
	to := time.Date(by, bm, bd, 0, 0, 0, 0, time.UTC)
	if to.Before(from) {
		return 0
	}
	return int(to.Sub(from) / hoursInOneDay)
}

// Next schedules a review of c with rating g at time now. The local day is
// read from now's time zone. Due is returned in UTC.
func Next(c Card, g int, now time.Time, retention float64) Result {
	s, d := memory(c, g, now)
	left := stepsLeft(c)

	var res Result
	res.S, res.D = s, d
	switch {
	case left == 0 && g == Again:
		// A lapse: one relearning step.
		res.Step = 1
		res.Due = now.Add(relearnStep)
	case left == 0:
		res.Due = inDays(now, reviewInterval(c, g, now, retention))
	case g == Easy, g == Good && left == 1:
		res.Due = inDays(now, Interval(s, retention))
	case g == Good:
		res.Step = left - 1
		res.Due = now.Add(secondStep)
	case g == Hard:
		res.Step = left
		res.Due = now.Add(pick(c.relearning(), hardRelearn, hardLearning))
	default: // Again on a step: back to the first step.
		res.Step = pick(c.relearning(), 1, LearningSteps)
		res.Due = now.Add(pick(c.relearning(), relearnStep, firstStep))
	}
	res.Due = res.Due.UTC()
	return res
}

// relearning reports whether a card on a step got there by a lapse.
func (c Card) relearning() bool { return c.Reviewed && c.Step > 0 && c.Lapsed }

// stepsLeft is the number of good ratings c needs to reach review.
func stepsLeft(c Card) int {
	if !c.Reviewed {
		return LearningSteps
	}
	return c.Step
}

// memory returns stability and difficulty after rating g.
func memory(c Card, g int, now time.Time) (s, d float64) {
	if !c.Reviewed {
		return clamp(math.Max(w[g-1], firstFloor), minStability, maxStability), clamp(d0(g), 1, 10)
	}
	return clamp(stability(c, g, Days(c.Last, now)), minStability, maxStability), difficulty(c.D, g)
}

// d0 is the starting difficulty for a first rating of g, not clamped.
func d0(g int) float64 {
	return w[4] - math.Exp(w[5]*float64(g-1)) + 1
}

func difficulty(d float64, g int) float64 {
	next := d - w[6]*float64(g-3)*(10-d)/9
	return clamp(w[7]*d0(Easy)+(1-w[7])*next, 1, 10)
}

// stability applies the same-day, recalled, or lapsed formula. It uses the
// difficulty from before the review.
func stability(c Card, g, days int) float64 {
	s, d := c.S, c.D
	if days == 0 {
		m := math.Exp(w[17]*(float64(g-3)+w[18])) * math.Pow(s, -w[19])
		if g >= Hard {
			m = math.Max(m, 1)
		}
		return s * m
	}
	r := Recall(float64(days), s)
	if g == Again {
		forgot := w[11] * math.Pow(d, -w[12]) * (math.Pow(s+1, w[13]) - 1) * math.Exp(w[14]*(1-r))
		return math.Min(forgot, s/math.Exp(w[17]*w[18]))
	}
	h, e := 1.0, 1.0
	if g == Hard {
		h = w[15]
	}
	if g == Easy {
		e = w[16]
	}
	return s * (1 + math.Exp(w[8])*(11-d)*math.Pow(s, -w[9])*(math.Exp(w[10]*(1-r))-1)*h*e)
}

// reviewInterval is the interval for a card in review rated hard, good, or
// easy. All three are computed and ordered: hard is at most good, good is at
// least hard plus 1, and easy is at least good plus 1. The result stays within
// the 36,500-day ceiling.
func reviewInterval(c Card, g int, now time.Time, retention float64) int {
	days := Days(c.Last, now)
	of := func(rating int) int {
		return Interval(clamp(stability(c, rating, days), minStability, maxStability), retention)
	}
	hard, good, easy := of(Hard), of(Good), of(Easy)
	hard = min(hard, good)
	good = max(good, hard+1)
	easy = max(easy, good+1)
	days = easy
	switch g {
	case Hard:
		days = hard
	case Good:
		days = good
	}
	// The ordering can push past the ceiling when all three sit on it.
	return min(days, maxInterval)
}

// inDays is the same wall-clock time n calendar days after now.
func inDays(now time.Time, n int) time.Time {
	return now.AddDate(0, 0, n)
}

func clamp(x, lo, hi float64) float64 {
	return math.Min(math.Max(x, lo), hi)
}

func pick[T any](cond bool, yes, no T) T {
	if cond {
		return yes
	}
	return no
}
