package main

import (
	"fmt"
	"os"
	"time"
)

var logTimestamp = true

var levelColor = map[string]string{
	"WARN":  "\x1b[33m",
	"ERROR": "\x1b[31m",
	"FATAL": "\x1b[35m",
}

const colorReset = "\x1b[0m"

func logf(level, format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	coloredLevel := level
	if c, ok := levelColor[level]; ok {
		coloredLevel = c + level + colorReset
	}
	if logTimestamp {
		now := time.Now()
		_, _ = fmt.Fprintf(os.Stderr, "%s %s %s %s %s\n",
			now.Format("-0700"),
			now.Format("2006-01-02"),
			now.Format("15:04:05.000"),
			coloredLevel, msg)
	} else {
		_, _ = fmt.Fprintf(os.Stderr, "%s %s\n", coloredLevel, msg)
	}
}
