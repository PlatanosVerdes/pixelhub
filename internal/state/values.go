package state

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Duration is written like "2m" or "3h".
type Duration time.Duration

func (d Duration) MarshalText() ([]byte, error) { return []byte(time.Duration(d).String()), nil }

func (d *Duration) UnmarshalText(text []byte) error {
	v, err := time.ParseDuration(string(text))
	if err != nil {
		return fmt.Errorf("%q is not a duration like 2m", text)
	}
	*d = Duration(v)
	return nil
}

// Clock is minutes after midnight, written HH:MM; 24:00 ends a range at midnight.
type Clock int

func parseClock(s string) (Clock, error) {
	h, m, ok := strings.Cut(s, ":")
	hour, errH := strconv.Atoi(h)
	minute, errM := strconv.Atoi(m)
	if !ok || errH != nil || errM != nil || hour < 0 || minute < 0 || minute > 59 || hour*60+minute > 24*60 {
		return 0, fmt.Errorf("%q is not a time like 07:30", s)
	}
	return Clock(hour*60 + minute), nil
}

func (c Clock) String() string { return fmt.Sprintf("%02d:%02d", c/60, c%60) }

func (c Clock) MarshalText() ([]byte, error) { return []byte(c.String()), nil }

func (c *Clock) UnmarshalText(text []byte) error {
	v, err := parseClock(string(text))
	*c = v
	return err
}

// Days is a weekday set written like "mon-fri", "sat,sun" or "daily"; empty means every day.
type Days uint8

const everyDay Days = 1<<7 - 1

var dayNames = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

func (d Days) Has(w time.Weekday) bool { return d == 0 || d&(1<<w) != 0 }

func ParseDays(s string) (Days, error) {
	var set Days
	for part := range strings.SplitSeq(strings.ToLower(s), ",") {
		part = strings.TrimSpace(part)
		if part == "daily" || part == "" {
			continue
		}
		from, to, isRange := strings.Cut(part, "-")
		a, b := day(from), day(to)
		if !isRange {
			b = a
		}
		if a < 0 || b < 0 {
			return 0, fmt.Errorf("%q is not a day set like mon-fri or sat,sun", s)
		}
		for i := a; ; i = (i + 1) % 7 {
			set |= 1 << i
			if i == b {
				break
			}
		}
	}
	return set, nil
}

func day(name string) int { return slices.Index(dayNames, strings.TrimSpace(name)) }

// String writes runs of three or more days as ranges, starting the week on Monday.
func (d Days) String() string {
	if d == 0 || d == everyDay {
		return "daily"
	}
	order := []int{1, 2, 3, 4, 5, 6, 0}
	var parts []string
	for i := 0; i < len(order); {
		if d&(1<<order[i]) == 0 {
			i++
			continue
		}
		j := i
		for j+1 < len(order) && d&(1<<order[j+1]) != 0 {
			j++
		}
		if j-i >= 2 {
			parts = append(parts, dayNames[order[i]]+"-"+dayNames[order[j]])
		} else {
			for k := i; k <= j; k++ {
				parts = append(parts, dayNames[order[k]])
			}
		}
		i = j + 1
	}
	return strings.Join(parts, ",")
}

func (d Days) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

func (d *Days) UnmarshalText(text []byte) error {
	v, err := ParseDays(string(text))
	*d = v
	return err
}

// Level is a brightness 0-100, or "off" for 0.
type Level int

func (l *Level) UnmarshalJSON(data []byte) error {
	if string(data) == `"off"` {
		*l = 0
		return nil
	}
	v, err := strconv.Atoi(string(data))
	if err != nil || v < 0 || v > 100 {
		return fmt.Errorf("brightness %s is not 0-100 or \"off\"", data)
	}
	*l = Level(v)
	return nil
}
