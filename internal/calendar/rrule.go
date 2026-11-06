package calendar

import (
	"iter"
	"slices"
	"strconv"
	"strings"
	"time"
)

type rule struct {
	freq     string
	interval int
	count    int
	until    time.Time
	byDay    []weekday
	monthDay []int
	weekFrom time.Weekday
}

// weekday is a BYDAY entry: MO, or 2MO and -1FR for the nth weekday of a month.
type weekday struct {
	day time.Weekday
	nth int
}

var weekdays = map[string]time.Weekday{"SU": time.Sunday, "MO": time.Monday, "TU": time.Tuesday, "WE": time.Wednesday,
	"TH": time.Thursday, "FR": time.Friday, "SA": time.Saturday}

const maxPeriods = 20000

func parseRule(s string, start time.Time) (rule, bool) {
	r := rule{interval: 1, weekFrom: time.Monday}
	for part := range strings.SplitSeq(s, ";") {
		k, v, _ := strings.Cut(part, "=")
		switch strings.ToUpper(k) {
		case "FREQ":
			r.freq = strings.ToUpper(v)
		case "INTERVAL":
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				r.interval = n
			}
		case "COUNT":
			r.count, _ = strconv.Atoi(v)
		case "UNTIL":
			params := map[string]string{}
			if !strings.HasSuffix(v, "Z") {
				params["TZID"] = start.Location().String()
			}
			if t, allDay, err := parseTime(v, params); err == nil {
				if allDay {
					t = t.AddDate(0, 0, 1).Add(-time.Second)
				}
				r.until = t
			}
		case "BYDAY":
			for d := range strings.SplitSeq(v, ",") {
				d = strings.ToUpper(strings.TrimSpace(d))
				if len(d) < 2 {
					continue
				}
				wd, ok := weekdays[d[len(d)-2:]]
				if !ok {
					continue
				}
				nth, _ := strconv.Atoi(d[:len(d)-2])
				r.byDay = append(r.byDay, weekday{wd, nth})
			}
		case "BYMONTHDAY":
			for d := range strings.SplitSeq(v, ",") {
				if n, err := strconv.Atoi(d); err == nil && n != 0 {
					r.monthDay = append(r.monthDay, n)
				}
			}
		case "WKST":
			if wd, ok := weekdays[strings.ToUpper(v)]; ok {
				r.weekFrom = wd
			}
		}
	}
	switch r.freq {
	case "DAILY", "WEEKLY", "MONTHLY", "YEARLY":
		return r, true
	}
	return r, false
}

// expand yields the starts of a recurring event up to horizon, in order, COUNT and UNTIL applied.
// It knows the rules calendars write in practice; an unknown frequency yields only the first start.
func expand(start time.Time, rrule string, horizon time.Time) iter.Seq[time.Time] {
	return func(yield func(time.Time) bool) {
		r, ok := parseRule(rrule, start)
		if !ok {
			yield(start)
			return
		}
		seen := 0
		for i := range maxPeriods {
			candidates := r.period(start, i)
			if len(candidates) > 0 && !candidates[0].Before(horizon) {
				return
			}
			for _, t := range candidates {
				if t.Before(start) {
					continue
				}
				if (!r.until.IsZero() && t.After(r.until)) || (r.count > 0 && seen >= r.count) || !t.Before(horizon) {
					return
				}
				seen++
				if !yield(t) {
					return
				}
			}
		}
	}
}

// period lists, sorted, the starts that fall in the i-th period of the rule.
func (r rule) period(start time.Time, i int) []time.Time {
	at := func(y int, m time.Month, d int) (time.Time, bool) {
		t := time.Date(y, m, d, start.Hour(), start.Minute(), start.Second(), 0, start.Location())
		return t, t.Day() == d && t.Month() == m
	}
	var out []time.Time
	keep := func(t time.Time, ok bool) {
		if ok {
			out = append(out, t)
		}
	}
	switch r.freq {
	case "DAILY":
		t := start.AddDate(0, 0, i*r.interval)
		if len(r.byDay) == 0 || slices.ContainsFunc(r.byDay, func(w weekday) bool { return w.day == t.Weekday() }) {
			out = append(out, t)
		}
	case "WEEKLY":
		offset := (int(start.Weekday()) - int(r.weekFrom) + 7) % 7
		week := start.AddDate(0, 0, -offset+7*i*r.interval)
		if len(r.byDay) == 0 {
			out = append(out, start.AddDate(0, 0, 7*i*r.interval))
			break
		}
		for _, w := range r.byDay {
			d := week.AddDate(0, 0, (int(w.day)-int(r.weekFrom)+7)%7)
			keep(at(d.Year(), d.Month(), d.Day()))
		}
	case "MONTHLY":
		first := time.Date(start.Year(), start.Month()+time.Month(i*r.interval), 1, 0, 0, 0, 0, start.Location())
		y, m := first.Year(), first.Month()
		days := time.Date(y, m+1, 0, 0, 0, 0, 0, start.Location()).Day()
		switch {
		case len(r.monthDay) > 0:
			for _, d := range r.monthDay {
				if d < 0 {
					d = days + 1 + d
				}
				keep(at(y, m, d))
			}
		case len(r.byDay) > 0:
			for _, w := range r.byDay {
				for _, d := range monthWeekdays(y, m, days, w) {
					keep(at(y, m, d))
				}
			}
		default:
			keep(at(y, m, start.Day()))
		}
	case "YEARLY":
		keep(at(start.Year()+i*r.interval, start.Month(), start.Day()))
	}
	slices.SortFunc(out, func(a, b time.Time) int { return a.Compare(b) })
	return slices.CompactFunc(out, time.Time.Equal)
}

// monthWeekdays are the days of month m that match w: every such weekday, or only the nth one.
func monthWeekdays(y int, m time.Month, days int, w weekday) []int {
	var all []int
	for d := 1; d <= days; d++ {
		if time.Date(y, m, d, 12, 0, 0, 0, time.UTC).Weekday() == w.day {
			all = append(all, d)
		}
	}
	switch {
	case w.nth == 0:
		return all
	case w.nth > 0 && w.nth <= len(all):
		return all[w.nth-1 : w.nth]
	case w.nth < 0 && -w.nth <= len(all):
		return all[len(all)+w.nth : len(all)+w.nth+1]
	}
	return nil
}
