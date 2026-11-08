package talk

import (
	"time"

	"github.com/PlatanosVerdes/pixelhub/internal/display"
)

type idlePose struct {
	shape Shape
	mood  Mood
	ms    int
}

// idleLoop is Mosko waiting: blinks at uneven gaps, a double one, and a look aside now and then.
var idleLoop = []idlePose{
	{Shut, Calm, 2600}, {Blink, Calm, 140},
	{Shut, Calm, 3400}, {Blink, Calm, 120}, {Shut, Calm, 180}, {Blink, Calm, 120},
	{Shut, Calm, 2200}, {Shut, Thinking, 1300},
	{Shut, Calm, 1900}, {Blink, Calm, 140},
	{Shut, Calm, 2800}, {Blink, Calm, 140},
}

// Idle is Mosko waiting on the display, looping by itself, so it needs nothing sent while it plays.
func Idle() display.Content {
	c := display.Content{Kind: display.Picture, Name: "Mosko", Key: "mosko-idle"}
	for _, p := range idleLoop {
		c.Frames = append(c.Frames, display.Frame{Image: Face(p.shape, p.mood), Delay: time.Duration(p.ms) * time.Millisecond})
	}
	return c
}
