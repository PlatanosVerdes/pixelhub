// Package hfp plays the audio gateway side of the Hands-Free Profile the way an iPhone does, which is
// what makes a hands-free unit report its battery with Apple's AT+IPHONEACCEV.
package hfp

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// Indicators the unit asks about; only their order matters to it.
const (
	indicatorRanges = `+CIND: ("service",(0-1)),("call",(0-1)),("callsetup",(0-3)),("battchg",(0-5)),("signal",(0-5)),("roam",(0-1)),("callheld",(0-2))`
	indicatorValues = "+CIND: 1,0,0,3,2,0,0"
)

// appleBattery is the IPHONEACCEV key for the battery level, 0 (empty) to 9 (full).
const appleBattery = "1"

// Serve answers the unit on rw until reading fails, calling battery with each level it reports.
func Serve(rw io.ReadWriter, now func() time.Time, battery func(level int)) error {
	in := bufio.NewScanner(rw)
	in.Split(splitCR)
	for in.Scan() {
		cmd := strings.TrimSpace(in.Text())
		if cmd == "" {
			continue
		}
		replies := append(answer(cmd, now()), "OK")
		for _, r := range replies {
			if _, err := io.WriteString(rw, "\r\n"+r+"\r\n"); err != nil {
				return fmt.Errorf("hands-free write: %w", err)
			}
		}
		if level, ok := batteryLevel(cmd); ok {
			battery(level)
		}
	}
	if err := in.Err(); err != nil {
		return fmt.Errorf("hands-free read: %w", err)
	}
	return io.EOF
}

func answer(cmd string, now time.Time) []string {
	switch {
	case strings.HasPrefix(cmd, "AT+BRSF="):
		return []string{"+BRSF:1007"}
	case cmd == "AT+CIND=?":
		return []string{indicatorRanges}
	case cmd == "AT+CIND?":
		return []string{indicatorValues}
	case cmd == "AT+CHLD=?":
		return []string{"+CHLD: (0,1,1x,2,2x,3)"}
	case cmd == "AT+CGMI":
		return []string{`+CGMI: "Apple Inc."`}
	case cmd == "AT+CGMM":
		return []string{"+CGMM: iPhone16,1"}
	case cmd == "AT+CCLK?":
		return []string{now.Format(`+CCLK: "06/01/02, 15:04:05"`)}
	case strings.HasPrefix(cmd, "AT+XAPL="):
		return []string{"+XAPL=iPhone,7"}
	}
	return nil
}

// batteryLevel reads AT+IPHONEACCEV=<count>,<key>,<value>,...
func batteryLevel(cmd string) (int, bool) {
	args, ok := strings.CutPrefix(cmd, "AT+IPHONEACCEV=")
	if !ok {
		return 0, false
	}
	fields := strings.Split(args, ",")
	for i := 1; i+1 < len(fields); i += 2 {
		if strings.TrimSpace(fields[i]) != appleBattery {
			continue
		}
		level, err := strconv.Atoi(strings.TrimSpace(fields[i+1]))
		if err != nil || level < 0 || level > 9 {
			return 0, false
		}
		return level, true
	}
	return 0, false
}

func splitCR(data []byte, atEOF bool) (int, []byte, error) {
	for i, b := range data {
		if b == '\r' || b == '\n' {
			return i + 1, data[:i], nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}
