package main

import "math/rand/v2"

// Very frivolous and not very minimal time-zone callbacks to The West Wing,
// "20 Hours in America, Part I".
var timeZoneJokes = [...]string{
	"I work at the White House.",
	"What? People, they just... They... They reset their watches when they commute?",
	"Can we have a civilisation?",
}

// Choose once per process so redraws and navigation keep the same punchline.
var startupJoke = timeZoneJokes[rand.IntN(len(timeZoneJokes))]
