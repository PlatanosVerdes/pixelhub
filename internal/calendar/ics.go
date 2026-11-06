package calendar

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Event is one occurrence of a calendar event.
type Event struct {
	Title string    `json:"title"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	// Alarms are the event's own reminders, as minutes before it starts.
	Alarms []int `json:"alarms"`
	uid    string
}

type vevent struct {
	uid, summary string
	start, end   time.Time
	allDay       bool
	cancelled    bool
	declined     bool
	rrule        string
	exdates      []time.Time
	alarms       []alarm
	// recurrenceID, when set, is the start of the recurring instance this one replaces.
	recurrenceID time.Time
}

// alarm is a VALARM trigger: an offset from the start or the end, or a fixed time.
type alarm struct {
	offset   time.Duration
	fromEnd  bool
	absolute time.Time
}

type property struct {
	name   string
	params map[string]string
	value  string
}

// parse reads the VEVENTs of an iCalendar feed; me is the owner's address, for declined invitations.
func parse(r io.Reader, me string) ([]vevent, error) {
	lines, err := unfold(r)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 || !strings.EqualFold(lines[0], "BEGIN:VCALENDAR") {
		return nil, errors.New("not an iCalendar feed")
	}
	var events []vevent
	var cur *vevent
	// trigger is the VALARM being read, nil outside one or until it has a usable TRIGGER.
	var trigger *alarm
	inAlarm := false
	depth := 0
	for _, line := range lines {
		p, ok := parseLine(line)
		if !ok {
			continue
		}
		switch {
		case p.name == "BEGIN" && strings.EqualFold(p.value, "VEVENT"):
			cur, depth = &vevent{}, 0
			continue
		case cur == nil:
			continue
		case p.name == "BEGIN":
			depth++
			inAlarm = depth == 1 && strings.EqualFold(p.value, "VALARM")
			continue
		case p.name == "END" && depth > 0:
			depth--
			if depth == 0 && trigger != nil {
				cur.alarms = append(cur.alarms, *trigger)
			}
			if depth == 0 {
				trigger, inAlarm = nil, false
			}
			continue
		case depth == 1 && inAlarm && p.name == "TRIGGER":
			if a, ok := parseTrigger(p); ok {
				trigger = &a
			}
			continue
		case p.name == "END" && strings.EqualFold(p.value, "VEVENT"):
			if !cur.start.IsZero() {
				events = append(events, *cur)
			}
			cur = nil
			continue
		case depth > 0:
			continue
		}
		cur.apply(p, me)
	}
	return events, nil
}

func (e *vevent) apply(p property, me string) {
	switch p.name {
	case "UID":
		e.uid = p.value
	case "SUMMARY":
		e.summary = unescape(p.value)
	case "DTSTART":
		e.start, e.allDay, _ = parseTime(p.value, p.params)
	case "DTEND":
		e.end, _, _ = parseTime(p.value, p.params)
	case "DURATION":
		if d, err := parseDuration(p.value); err == nil && !e.start.IsZero() {
			e.end = e.start.Add(d)
		}
	case "STATUS":
		e.cancelled = strings.EqualFold(p.value, "CANCELLED")
	case "RRULE":
		e.rrule = p.value
	case "EXDATE":
		for v := range strings.SplitSeq(p.value, ",") {
			if t, _, err := parseTime(v, p.params); err == nil {
				e.exdates = append(e.exdates, t)
			}
		}
	case "RECURRENCE-ID":
		e.recurrenceID, _, _ = parseTime(p.value, p.params)
	case "ATTENDEE":
		if me != "" && strings.EqualFold(strings.TrimPrefix(strings.ToLower(p.value), "mailto:"), me) &&
			strings.EqualFold(p.params["PARTSTAT"], "DECLINED") {
			e.declined = true
		}
	}
}

// unfold joins the continuation lines of a feed, which start with a space or a tab.
func unfold(r io.Reader) ([]string, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	var lines []string
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) && len(lines) > 0 {
			lines[len(lines)-1] += line[1:]
			continue
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines, sc.Err()
}

func parseLine(line string) (property, bool) {
	colon := -1
	quoted := false
	for i, c := range line {
		if c == '"' {
			quoted = !quoted
		} else if c == ':' && !quoted {
			colon = i
			break
		}
	}
	if colon < 0 {
		return property{}, false
	}
	parts := strings.Split(line[:colon], ";")
	p := property{name: strings.ToUpper(parts[0]), params: map[string]string{}, value: line[colon+1:]}
	for _, kv := range parts[1:] {
		if k, v, ok := strings.Cut(kv, "="); ok {
			p.params[strings.ToUpper(k)] = strings.Trim(v, `"`)
		}
	}
	return p, true
}

func parseTrigger(p property) (alarm, bool) {
	if strings.EqualFold(p.params["VALUE"], "DATE-TIME") {
		t, _, err := parseTime(p.value, p.params)
		return alarm{absolute: t}, err == nil
	}
	d, err := parseDuration(p.value)
	return alarm{offset: d, fromEnd: strings.EqualFold(p.params["RELATED"], "END")}, err == nil
}

// minutesBefore is how long before start the alarm goes off, in whole minutes; false if it goes off after.
func (a alarm) minutesBefore(start, end time.Time) (int, bool) {
	at := start.Add(a.offset)
	switch {
	case !a.absolute.IsZero():
		at = a.absolute
	case a.fromEnd:
		at = end.Add(a.offset)
	}
	before := start.Sub(at)
	if before <= 0 {
		return 0, false
	}
	return int((before + time.Minute - 1) / time.Minute), true
}

func unescape(s string) string {
	return strings.NewReplacer(`\n`, " ", `\N`, " ", `\,`, ",", `\;`, ";", `\\`, `\`).Replace(s)
}

// parseTime reads a DATE or DATE-TIME value: UTC with a Z, in its TZID, or floating in local time.
func parseTime(v string, params map[string]string) (t time.Time, allDay bool, err error) {
	loc := time.Local
	if tz := params["TZID"]; tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	switch {
	case params["VALUE"] == "DATE" || len(v) == 8:
		t, err = time.ParseInLocation("20060102", v, loc)
		return t, true, err
	case strings.HasSuffix(v, "Z"):
		t, err = time.Parse("20060102T150405Z", v)
		return t, false, err
	}
	t, err = time.ParseInLocation("20060102T150405", v, loc)
	return t, false, err
}

// parseDuration reads the usual forms of an RFC 5545 duration, like PT1H30M or P1D.
func parseDuration(v string) (time.Duration, error) {
	neg := strings.HasPrefix(v, "-")
	v = strings.TrimLeft(v, "+-")
	if !strings.HasPrefix(v, "P") {
		return 0, fmt.Errorf("duration %q", v)
	}
	var d time.Duration
	num := ""
	for _, c := range v[1:] {
		unit := time.Duration(0)
		switch c {
		case 'T':
			continue
		case 'W':
			unit = 7 * 24 * time.Hour
		case 'D':
			unit = 24 * time.Hour
		case 'H':
			unit = time.Hour
		case 'M':
			unit = time.Minute
		case 'S':
			unit = time.Second
		default:
			num += string(c)
			continue
		}
		n, err := strconv.Atoi(num)
		if err != nil {
			return 0, fmt.Errorf("duration %q", v)
		}
		d, num = d+time.Duration(n)*unit, ""
	}
	if neg {
		d = -d
	}
	return d, nil
}

// occurrences lists the timed events that start in [from, to), recurring ones expanded, with
// cancelled, declined and all-day ones left out, sorted by start.
func occurrences(events []vevent, from, to time.Time) []Event {
	type key struct {
		uid   string
		start int64
	}
	moved := map[key]bool{}
	for _, e := range events {
		if !e.recurrenceID.IsZero() {
			moved[key{e.uid, e.recurrenceID.Unix()}] = true
		}
	}
	var out []Event
	add := func(e vevent, start time.Time) {
		if e.cancelled || e.declined || e.allDay || start.Before(from) || !start.Before(to) {
			return
		}
		end := start
		if !e.end.IsZero() {
			end = start.Add(e.end.Sub(e.start))
		}
		alarms := []int{}
		for _, a := range e.alarms {
			if a.absolute.IsZero() || e.rrule == "" {
				if m, ok := a.minutesBefore(start, end); ok && !slices.Contains(alarms, m) {
					alarms = append(alarms, m)
				}
			}
		}
		slices.Sort(alarms)
		out = append(out, Event{Title: e.summary, Start: start, End: end, Alarms: alarms, uid: e.uid})
	}
	for _, e := range events {
		if e.rrule == "" || !e.recurrenceID.IsZero() {
			add(e, e.start)
			continue
		}
		for start := range expand(e.start, e.rrule, to) {
			if moved[key{e.uid, start.Unix()}] || slices.ContainsFunc(e.exdates, start.Equal) {
				continue
			}
			add(e, start)
		}
	}
	slices.SortFunc(out, func(a, b Event) int { return a.Start.Compare(b.Start) })
	return out
}
