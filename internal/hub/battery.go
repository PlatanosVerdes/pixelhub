package hub

import "time"

// batteryBand is the picture for a level of the display's 0-9 battery and how often it repeats.
// A band that only climbs announce is quiet when it is the first level seen: a full battery may be
// resting there off the charger.
func batteryBand(level int) (sprite string, every time.Duration, climbOnly bool) {
	switch {
	case level <= 0:
		return "battery-dying", time.Minute, false
	case level == 1:
		return "battery-charge", 5 * time.Minute, false
	case level >= 8:
		return "battery-unplug", 15 * time.Minute, true
	}
	return "", 0, false
}

// batteryWatch shows a band's picture when the level enters it, then again every while it stays.
type batteryWatch struct {
	seen    bool
	band    string
	shownAt time.Time
	quiet   bool
}

// see takes the level reported now and returns the picture to show, if any.
func (w *batteryWatch) see(level int, now time.Time) string {
	sprite, every, climbOnly := batteryBand(level)
	if sprite != w.band {
		w.band, w.shownAt, w.quiet = sprite, time.Time{}, climbOnly && !w.seen
	}
	w.seen = true
	if sprite == "" || w.quiet || (!w.shownAt.IsZero() && now.Sub(w.shownAt) < every) {
		return ""
	}
	w.shownAt = now
	return sprite
}

// notChargingEvery is how often the plug-on-but-not-charging picture repeats while it lasts.
const notChargingEvery = 15 * time.Minute

// watchBattery puts the battery's picture on screen, above anything picked by hand.
func (h *Hub) watchBattery(now time.Time) {
	reported := h.display.Status().Battery
	if reported == nil {
		return
	}
	sprite := h.battery.see(*reported, now)
	if sprite == "" && h.Plug != nil {
		if st := h.Plug.Status(); st != nil && st.NotCharging {
			if h.notChargingAt.IsZero() || now.Sub(h.notChargingAt) >= notChargingEvery {
				h.notChargingAt, sprite = now, "battery-not-charging"
			}
		} else {
			h.notChargingAt = time.Time{}
		}
	}
	if sprite == "" {
		return
	}
	c, ok := h.sprites.Get(sprite)
	if !ok {
		return
	}
	h.log.Info("battery alert", "level", *reported, "sprite", sprite)
	h.mu.Lock()
	h.alerts.push(Alert{Content: c, Priority: batteryPriority, Duration: 10 * time.Second, Source: "battery"}, now)
	h.mu.Unlock()
}
