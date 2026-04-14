package main

import (
	"fmt"
	"os"
	"time"
)

var levelColor = map[string]string{
	"WARN":  "\x1b[33m",
	"ERROR": "\x1b[31m",
	"FATAL": "\x1b[35m",
}

const colorReset = "\x1b[0m"

type logfFunc func(level, format string, a ...any)

func newLogf(timestamp, disableColor bool) logfFunc {
	var colorize func(level string) string
	if disableColor {
		colorize = func(level string) string {
			return level
		}
	} else {
		colorize = func(level string) string {
			if c, ok := levelColor[level]; ok {
				return c + level + colorReset
			}
			return level
		}
	}
	if timestamp {
		return func(level, format string, a ...any) {
			now := time.Now()
			_, _ = fmt.Fprintf(os.Stderr, "%s %s %s %s %s\n",
				now.Format("-0700"),
				now.Format("2006-01-02"),
				now.Format("15:04:05.000"),
				colorize(level), fmt.Sprintf(format, a...))
		}
	}
	return func(level, format string, a ...any) {
		_, _ = fmt.Fprintf(os.Stderr, "%s %s\n", colorize(level), fmt.Sprintf(format, a...))
	}
}
