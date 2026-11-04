package sun

import (
	"math"
	"time"
)

type Phase int

const (
	Night Phase = iota
	Dusk
	Day
)

func (p Phase) String() string {
	return [...]string{"night", "dusk", "day"}[p]
}

const (
	zenithSunrise = 90.833
	zenithCivil   = 96
)

// Times returns sunrise and sunset on t's calendar day, in t's location. ok is false on a polar day or night.
func Times(t time.Time, lat, lon float64) (rise, set time.Time, ok bool) {
	return crossings(t, lat, lon, zenithSunrise)
}

func PhaseAt(t time.Time, lat, lon float64) Phase {
	rise, set, ok := Times(t, lat, lon)
	if !ok {
		if declinationSign(t) == math.Signbit(lat) {
			return Day
		}
		return Night
	}
	if !t.Before(rise) && t.Before(set) {
		return Day
	}
	dawn, dusk, ok := crossings(t, lat, lon, zenithCivil)
	if ok && !t.Before(dawn) && t.Before(dusk) {
		return Dusk
	}
	return Night
}

// crossings follows NOAA's solar calculator: one evaluation at local noon is accurate to about a minute.
func crossings(t time.Time, lat, lon, zenith float64) (morning, evening time.Time, ok bool) {
	midnight := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	decl, eqTime := solar(midnight.Add(12*time.Hour - time.Duration(lon/15*float64(time.Hour))))
	cosHA := math.Cos(rad(zenith))/(math.Cos(rad(lat))*math.Cos(decl)) - math.Tan(rad(lat))*math.Tan(decl)
	if cosHA < -1 || cosHA > 1 {
		return time.Time{}, time.Time{}, false
	}
	ha := deg(math.Acos(cosHA))
	noon := 720 - 4*lon - eqTime
	at := func(minutes float64) time.Time {
		return midnight.Add(time.Duration(minutes * float64(time.Minute))).In(t.Location())
	}
	return at(noon - 4*ha), at(noon + 4*ha), true
}

func solar(t time.Time) (declination, eqTimeMinutes float64) {
	jd := float64(t.Unix())/86400 + 2440587.5
	c := (jd - 2451545) / 36525
	meanLong := math.Mod(280.46646+c*(36000.76983+c*0.0003032), 360)
	meanAnom := 357.52911 + c*(35999.05029-0.0001537*c)
	ecc := 0.016708634 - c*(0.000042037+0.0000001267*c)
	center := math.Sin(rad(meanAnom))*(1.914602-c*(0.004817+0.000014*c)) +
		math.Sin(rad(2*meanAnom))*(0.019993-0.000101*c) +
		math.Sin(rad(3*meanAnom))*0.000289
	omega := 125.04 - 1934.136*c
	appLong := meanLong + center - 0.00569 - 0.00478*math.Sin(rad(omega))
	meanObliq := 23 + (26+(21.448-c*(46.815+c*(0.00059-c*0.001813)))/60)/60
	obliq := meanObliq + 0.00256*math.Cos(rad(omega))
	declination = math.Asin(math.Sin(rad(obliq)) * math.Sin(rad(appLong)))
	y := math.Pow(math.Tan(rad(obliq/2)), 2)
	l0, m := rad(meanLong), rad(meanAnom)
	eqTimeMinutes = 4 * deg(y*math.Sin(2*l0)-2*ecc*math.Sin(m)+4*ecc*y*math.Sin(m)*math.Cos(2*l0)-
		0.5*y*y*math.Sin(4*l0)-1.25*ecc*ecc*math.Sin(2*m))
	return declination, eqTimeMinutes
}

func declinationSign(t time.Time) bool {
	decl, _ := solar(t)
	return math.Signbit(decl)
}

func rad(d float64) float64 { return d * math.Pi / 180 }
func deg(r float64) float64 { return r * 180 / math.Pi }
