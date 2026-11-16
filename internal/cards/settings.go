package cards

import (
	"fmt"
	"os"
	"strconv"

	"github.com/reithan/teach-me/internal/config"
	"github.com/reithan/teach-me/internal/fsrs"
)

// Settings are the flashcard settings of §12. tm and tmcards both read them.
type Settings struct {
	Retention float64 // target retention, 0.70 to 0.97
	New       int     // new cards introduced per local day
	Leech     int     // lapses after which a card is a leech
}

// Defaults.
const (
	DefaultNew   = 20
	DefaultLeech = 4
)

// LoadSettings reads each setting from its environment variable, else its
// config key (.tmconfig, then the user config), else the default. A learner
// often starts tmcards without the teacher's environment, which is why the
// config keys exist. A value that is set but not usable is an error whose
// text names where the value came from.
func LoadSettings() (Settings, error) {
	s := Settings{Retention: fsrs.DefaultRetention, New: DefaultNew, Leech: DefaultLeech}

	if raw, name, err := setting("TM_CARD_RETENTION", "card-retention"); err != nil {
		return s, err
	} else if raw != "" {
		r, perr := strconv.ParseFloat(raw, 64)
		if perr != nil || !fsrs.ValidRetention(r) {
			return s, fmt.Errorf("%s must be between 0.70 and 0.97, got %q", name, raw)
		}
		s.Retention = r
	}

	var err error
	if s.New, err = intSetting("TM_CARD_NEW", "card-new", DefaultNew, 0); err != nil {
		return s, err
	}
	if s.Leech, err = intSetting("TM_CARD_LEECH", "card-leech", DefaultLeech, 1); err != nil {
		return s, err
	}
	return s, nil
}

// setting returns the raw value and the name of the place it came from.
func setting(env, key string) (raw, name string, err error) {
	if v := os.Getenv(env); v != "" {
		return v, env, nil
	}
	v, ok, err := config.Lookup(key)
	if err != nil {
		return "", "", err
	}
	if !ok {
		return "", "", nil
	}
	return v, key, nil
}

func intSetting(env, key string, dflt, least int) (int, error) {
	raw, name, err := setting(env, key)
	if err != nil || raw == "" {
		return dflt, err
	}
	n, perr := strconv.Atoi(raw)
	if perr != nil || n < least {
		return dflt, fmt.Errorf("%s must be a whole number of at least %d, got %q", name, least, raw)
	}
	return n, nil
}
