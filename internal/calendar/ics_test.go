package calendar

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

const feed = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\n" +
	// A weekly stand-up on Monday and Wednesday in Madrid time, one Wednesday moved, one Monday dropped.
	"BEGIN:VEVENT\r\nUID:standup\r\nDTSTART;TZID=Europe/Madrid:20261102T093000\r\nDTEND;TZID=Europe/Madrid:20261102T094500\r\n" +
	"RRULE:FREQ=WEEKLY;BYDAY=MO,WE\r\nEXDATE;TZID=Europe/Madrid:20261109T093000\r\nSUMMARY:Stand-up\\, team\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:standup\r\nRECURRENCE-ID;TZID=Europe/Madrid:20261104T093000\r\nDTSTART;TZID=Europe/Madrid:20261104T110000\r\n" +
	"DTEND;TZID=Europe/Madrid:20261104T111500\r\nSUMMARY:Stand-up (late)\r\nEND:VEVENT\r\n" +
	// A one-off in UTC with an alarm inside, and a summary folded over two lines.
	"BEGIN:VEVENT\r\nUID:dentist\r\nDTSTART:20261103T150000Z\r\nDURATION:PT1H\r\nSUMMARY:Den\r\n tist\r\n" +
	"BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER:-P0DT0H30M0S\r\nSUMMARY:not the title\r\nEND:VALARM\r\n" +
	"BEGIN:VALARM\r\nACTION:EMAIL\r\nTRIGGER;VALUE=DATE-TIME:20261103T145500Z\r\nEND:VALARM\r\n" +
	"BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER;RELATED=END:-PT1H\r\nEND:VALARM\r\n" +
	"BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER:PT5M\r\nEND:VALARM\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:holiday\r\nDTSTART;VALUE=DATE:20261103\r\nDTEND;VALUE=DATE:20261104\r\nSUMMARY:Holiday\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:cancelled\r\nDTSTART:20261103T160000Z\r\nSTATUS:CANCELLED\r\nSUMMARY:Gone\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:declined\r\nDTSTART:20261103T170000Z\r\nSUMMARY:Nope\r\n" +
	"ATTENDEE;CN=Me;PARTSTAT=DECLINED:mailto:me@example.com\r\nEND:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

func TestOccurrences(t *testing.T) {
	t.Parallel()
	events, err := parse(strings.NewReader(feed), "me@example.com")
	if err != nil {
		t.Fatal(err)
	}
	madrid, _ := time.LoadLocation("Europe/Madrid")
	from := time.Date(2026, 11, 2, 0, 0, 0, 0, madrid)
	got := occurrences(events, from, from.AddDate(0, 0, 10))
	want := []struct {
		title string
		start time.Time
	}{
		{"Stand-up, team", time.Date(2026, 11, 2, 9, 30, 0, 0, madrid)},
		{"Dentist", time.Date(2026, 11, 3, 15, 0, 0, 0, time.UTC)},
		{"Stand-up (late)", time.Date(2026, 11, 4, 11, 0, 0, 0, madrid)},
		{"Stand-up, team", time.Date(2026, 11, 11, 9, 30, 0, 0, madrid)},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d events, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Title != w.title || !got[i].Start.Equal(w.start) {
			t.Errorf("event %d: got %q at %s, want %q at %s", i, got[i].Title, got[i].Start, w.title, w.start)
		}
	}
	if d := got[1].End.Sub(got[1].Start); d != time.Hour {
		t.Errorf("dentist lasts %s, want 1h", d)
	}
	// 30 min before, at 14:55 (5 min), and 1 h before the end (0, the start itself) is dropped, as is one after.
	if fmt.Sprint(got[1].Alarms) != "[5 30]" {
		t.Errorf("dentist alarms %v, want [5 30]", got[1].Alarms)
	}
	if len(got[0].Alarms) != 0 {
		t.Errorf("stand-up alarms %v, want none", got[0].Alarms)
	}
}

func TestExpand(t *testing.T) {
	t.Parallel()
	madrid, _ := time.LoadLocation("Europe/Madrid")
	start := time.Date(2026, 1, 31, 10, 0, 0, 0, madrid)
	tests := []struct {
		rule string
		want []string
	}{
		{"FREQ=DAILY;COUNT=3", []string{"2026-01-31", "2026-02-01", "2026-02-02"}},
		{"FREQ=DAILY;INTERVAL=2;UNTIL=20260204T090000Z", []string{"2026-01-31", "2026-02-02", "2026-02-04"}},
		{"FREQ=WEEKLY;INTERVAL=2;BYDAY=SA,TU;COUNT=4", []string{"2026-01-31", "2026-02-10", "2026-02-14", "2026-02-24"}},
		// The 31st is skipped in months without one.
		{"FREQ=MONTHLY;COUNT=3", []string{"2026-01-31", "2026-03-31", "2026-05-31"}},
		{"FREQ=MONTHLY;BYDAY=-1SA;COUNT=3", []string{"2026-01-31", "2026-02-28", "2026-03-28"}},
		{"FREQ=MONTHLY;BYMONTHDAY=1,-1;COUNT=3", []string{"2026-01-31", "2026-02-01", "2026-02-28"}},
		{"FREQ=YEARLY;COUNT=2", []string{"2026-01-31", "2027-01-31"}},
		{"FREQ=SECONDLY", []string{"2026-01-31"}},
	}
	for _, tt := range tests {
		var got []string
		for t := range expand(start, tt.rule, start.AddDate(2, 0, 0)) {
			if t.Hour() != 10 {
				got = append(got, "wrong hour "+t.String())
			}
			got = append(got, t.Format(time.DateOnly))
		}
		if strings.Join(got, " ") != strings.Join(tt.want, " ") {
			t.Errorf("%s: got %v, want %v", tt.rule, got, tt.want)
		}
	}
}

func TestDaylightSavingKeepsTheLocalHour(t *testing.T) {
	t.Parallel()
	madrid, _ := time.LoadLocation("Europe/Madrid")
	start := time.Date(2026, 10, 19, 9, 0, 0, 0, madrid)
	for t2 := range expand(start, "FREQ=WEEKLY;COUNT=3", start.AddDate(1, 0, 0)) {
		if t2.Hour() != 9 {
			t.Errorf("%s is not at 9:00 Madrid time", t2)
		}
	}
}

func TestOwner(t *testing.T) {
	t.Parallel()
	got := owner("https://calendar.google.com/calendar/ical/Someone%40gmail.com/private-abc123/basic.ics")
	if got != "someone@gmail.com" {
		t.Errorf("owner %q", got)
	}
	if got := owner("https://example.com/cal.ics"); got != "" {
		t.Errorf("owner %q, want none", got)
	}
}
