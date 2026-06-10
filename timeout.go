package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// unitDurations maps recognized unit spellings to their base duration.
var unitDurations = map[string]time.Duration{
	"":        time.Second, // bare number => seconds
	"s":       time.Second,
	"sec":     time.Second,
	"secs":    time.Second,
	"second":  time.Second,
	"seconds": time.Second,
	"m":       time.Minute,
	"min":     time.Minute,
	"mins":    time.Minute,
	"minute":  time.Minute,
	"minutes": time.Minute,
	"h":       time.Hour,
	"hr":      time.Hour,
	"hrs":     time.Hour,
	"hour":    time.Hour,
	"hours":   time.Hour,
	"d":       24 * time.Hour,
	"day":     24 * time.Hour,
	"days":    24 * time.Hour,
}

// termRE matches one "<number><optional spaces><unit>" term.
var termRE = regexp.MustCompile(`(?i)(\d+)\s*([a-z]*)`)

// parseTimeout parses a human-friendly duration string.
//
// Accepted forms include "123", "123s", "123 s", "123m", "123 min",
// hours and days analogously, and combinations such as "1h 5m".
func parseTimeout(s string) (time.Duration, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return 0, fmt.Errorf("empty timeout")
	}

	// Ensure the whole string is made up only of recognized terms by
	// stripping each match and checking nothing meaningful remains.
	remainder := termRE.ReplaceAllString(trimmed, "")
	if strings.TrimSpace(remainder) != "" {
		return 0, fmt.Errorf("invalid timeout %q", s)
	}

	matches := termRE.FindAllStringSubmatch(trimmed, -1)
	if len(matches) == 0 {
		return 0, fmt.Errorf("invalid timeout %q", s)
	}

	var total time.Duration
	for _, m := range matches {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return 0, fmt.Errorf("invalid number %q in timeout: %w", m[1], err)
		}
		unit, ok := unitDurations[strings.ToLower(m[2])]
		if !ok {
			return 0, fmt.Errorf("unknown unit %q in timeout", m[2])
		}
		total += time.Duration(n) * unit
	}

	if total <= 0 {
		return 0, fmt.Errorf("timeout must be positive")
	}
	return total, nil
}
