package main

import (
	"github.com/moonfruit/sing2seq/clef"
	"github.com/moonfruit/sing2seq/seq"
)

type orderedEvent = clef.Event

func newEvent() *clef.Event { return clef.NewEvent() }

func parseLine(raw string) *clef.Event { return clef.ParseSingBoxLine(raw) }

var _ = seq.NewSink
