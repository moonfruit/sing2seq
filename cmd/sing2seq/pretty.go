package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/moonfruit/sing2seq/clef"
)

var levelColor = map[string]string{
	"WARN":  "\x1b[33m",
	"ERROR": "\x1b[31m",
	"FATAL": "\x1b[35m",
}

const colorReset = "\x1b[0m"

var placeholderRe = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)

type prettyRenderer struct {
	timestamp    bool
	disableColor bool
}

func newPrettyRenderer(timestamp, disableColor bool) *prettyRenderer {
	return &prettyRenderer{timestamp: timestamp, disableColor: disableColor}
}

func (r *prettyRenderer) Match(*clef.Event) bool { return true }

func (r *prettyRenderer) Deliver(e *clef.Event) {
	level := levelShort(getString(e, "@l"))
	source := getString(e, "Source")
	module := getString(e, "Module")

	var head string
	if r.timestamp {
		now := parseRFC3339(getString(e, "@t"))
		head = fmt.Sprintf("%s %s %s %s",
			now.Format("-0700"),
			now.Format("2006-01-02"),
			now.Format("15:04:05.000"),
			r.colorize(level))
	} else {
		head = r.colorize(level)
	}

	body := r.renderBody(e, source, module)
	_, _ = fmt.Fprintf(os.Stderr, "%s %s\n", head, body)
}

// renderBody:
//   sing-box: <Module>[/<Type>][[<Tag>]]: <Detail>  (existing @mt template handles this)
//   sing2seq: sing2seq[/<Module>]: <Detail>
//   else:     fall back to @mt template
func (r *prettyRenderer) renderBody(e *clef.Event, source, module string) string {
	mt := getString(e, "@mt")
	switch source {
	case "sing-box":
		if mt != "" {
			return renderTemplate(e, mt)
		}
		return getString(e, "Detail")
	case "sing2seq":
		var b strings.Builder
		b.WriteString("sing2seq")
		if module != "" {
			b.WriteByte('/')
			b.WriteString(module)
		}
		b.WriteString(": ")
		if mt != "" {
			b.WriteString(renderTemplate(e, mt))
		} else {
			b.WriteString(getString(e, "Detail"))
		}
		return b.String()
	default:
		if mt != "" {
			return renderTemplate(e, mt)
		}
		return getString(e, "Detail")
	}
}

func (r *prettyRenderer) colorize(level string) string {
	if r.disableColor {
		return level
	}
	if c, ok := levelColor[level]; ok {
		return c + level + colorReset
	}
	return level
}

func renderTemplate(e *clef.Event, tmpl string) string {
	return placeholderRe.ReplaceAllStringFunc(tmpl, func(match string) string {
		name := match[1 : len(match)-1]
		if v, ok := e.Get(name); ok {
			return fmt.Sprintf("%v", v)
		}
		return match
	})
}

func getString(e *clef.Event, key string) string {
	if v, ok := e.Get(key); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func levelShort(clefName string) string {
	switch clefName {
	case "Verbose":
		return "TRACE"
	case "Debug":
		return "DEBUG"
	case "Information":
		return "INFO"
	case "Warning":
		return "WARN"
	case "Error":
		return "ERROR"
	case "Fatal":
		return "FATAL"
	default:
		return "INFO"
	}
}

func parseRFC3339(s string) time.Time {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	return time.Now()
}
