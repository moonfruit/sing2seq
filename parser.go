package main

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	ansiRe   = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	lineRe   = regexp.MustCompile(`^(?P<tz>[+-]\d{4})\s+(?P<date>\d{4}-\d{2}-\d{2})\s+(?P<time>\d{2}:\d{2}:\d{2}(?:\.\d+)?)\s+(?P<level>TRACE|DEBUG|INFO|WARN|ERROR|FATAL|PANIC)\s+(?:\[(?P<conn>\S+)\s+(?P<dur>[^\]]+)\]\s+)?(?P<rest>.*)$`)
	moduleRe = regexp.MustCompile(`^(?P<module>[a-zA-Z0-9_\-]+)(?:/(?P<type>[a-zA-Z0-9_\-]+))?(?:\[(?P<tag>[^\]]*)\])?:\s+(?P<detail>.*)$`)
)

var levelMap = map[string]string{
	"TRACE": "Verbose",
	"DEBUG": "Debug",
	"INFO":  "Information",
	"WARN":  "Warning",
	"ERROR": "Error",
	"FATAL": "Fatal",
	"PANIC": "Fatal",
}

func stripAnsi(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

type orderedEvent struct {
	keys   []string
	values map[string]any
}

func newEvent() *orderedEvent {
	return &orderedEvent{values: map[string]any{}}
}

func (e *orderedEvent) set(k string, v any) {
	if _, ok := e.values[k]; !ok {
		e.keys = append(e.keys, k)
	}
	e.values[k] = v
}

func namedMatches(re *regexp.Regexp, s string) map[string]string {
	m := re.FindStringSubmatch(s)
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(re.SubexpNames()))
	for i, name := range re.SubexpNames() {
		if name == "" {
			continue
		}
		out[name] = m[i]
	}
	return out
}

func parseLine(raw string) *orderedEvent {
	line := strings.TrimRight(stripAnsi(raw), "\r\n")
	if line == "" {
		return nil
	}

	m := namedMatches(lineRe, line)
	if m == nil {
		ev := newEvent()
		ev.set("@t", time.Now().Format("2006-01-02T15:04:05.000000-07:00"))
		ev.set("@l", "Information")
		ev.set("@mt", "{Raw}")
		ev.set("Raw", line)
		ev.set("Source", "sing-box")
		ev.set("Parsed", false)
		return ev
	}

	tz := m["tz"]
	ts := m["date"] + "T" + m["time"] + tz[:3] + ":" + tz[3:]
	level := levelMap[m["level"]]
	if level == "" {
		level = "Information"
	}

	ev := newEvent()
	ev.set("@t", ts)
	ev.set("@l", level)
	ev.set("Source", "sing-box")

	conn, hasConn := "", false
	if v, ok := m["conn"]; ok && v != "" {
		conn = v
		hasConn = true
		if n, err := strconv.Atoi(conn); err == nil {
			ev.set("ConnectionId", n)
		} else {
			ev.set("ConnectionId", conn)
		}
		ev.set("Duration", m["dur"])
	}

	rest := m["rest"]
	mm := namedMatches(moduleRe, rest)
	if mm != nil {
		ev.set("Module", mm["module"])
		if mm["type"] != "" {
			ev.set("Type", mm["type"])
		}
		hasTag := false
		if idx := moduleRe.SubexpIndex("tag"); idx >= 0 {
			sub := moduleRe.FindStringSubmatchIndex(rest)
			if sub != nil && sub[2*idx] >= 0 {
				hasTag = true
				ev.set("Tag", mm["tag"])
			}
		}
		ev.set("Detail", mm["detail"])

		var tmpl strings.Builder
		if hasConn {
			tmpl.WriteString("[{ConnectionId} {Duration}] ")
		}
		tmpl.WriteString("{Module}")
		if mm["type"] != "" {
			tmpl.WriteString("/{Type}")
		}
		if hasTag {
			tmpl.WriteString("[{Tag}]")
		}
		tmpl.WriteString(": {Detail}")
		ev.set("@mt", tmpl.String())
	} else {
		ev.set("Detail", rest)
		if hasConn {
			ev.set("@mt", "[{ConnectionId} {Duration}] {Detail}")
		} else {
			ev.set("@mt", "{Detail}")
		}
	}

	return ev
}
