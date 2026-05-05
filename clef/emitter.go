package clef

import "time"

// EmitterConfig 配置 Emitter。
type EmitterConfig struct {
	Source   string // 事件 Source 字段，例如 "sing2seq" / "daemon"
	MinLevel Level  // 低于此级别的事件被丢弃；不影响 PublishExternal
	Bus      *Bus   // 可选；nil 时事件被丢弃
}

// Emitter 是结构化事件的入口。所有调用都构造 Event 并发布到 Bus。
type Emitter struct {
	cfg EmitterConfig
}

// NewEmitter 创建 emitter。
func NewEmitter(cfg EmitterConfig) *Emitter {
	if cfg.Source == "" {
		cfg.Source = "app"
	}
	return &Emitter{cfg: cfg}
}

func (e *Emitter) Trace(module, eventID, mt string, fields map[string]any) {
	e.emit(LevelTrace, module, eventID, mt, fields)
}
func (e *Emitter) Debug(module, eventID, mt string, fields map[string]any) {
	e.emit(LevelDebug, module, eventID, mt, fields)
}
func (e *Emitter) Info(module, eventID, mt string, fields map[string]any) {
	e.emit(LevelInfo, module, eventID, mt, fields)
}
func (e *Emitter) Warn(module, eventID, mt string, fields map[string]any) {
	e.emit(LevelWarn, module, eventID, mt, fields)
}
func (e *Emitter) Error(module, eventID, mt string, fields map[string]any) {
	e.emit(LevelError, module, eventID, mt, fields)
}
func (e *Emitter) Fatal(module, eventID, mt string, fields map[string]any) {
	e.emit(LevelFatal, module, eventID, mt, fields)
}

// PublishExternal 接收已经构建好的事件（如 ParseSingBoxLine 返回的），
// 走与 Emitter 同一条出口（Bus）。MinLevel 不过滤外部事件。
func (e *Emitter) PublishExternal(ev *Event) {
	if e.cfg.Bus != nil {
		e.cfg.Bus.Publish(ev)
	}
}

func (e *Emitter) emit(level Level, module, eventID, mt string, fields map[string]any) {
	if level < e.cfg.MinLevel {
		return
	}
	ev := NewEvent()
	ev.Set("@t", time.Now().Format(time.RFC3339Nano))
	ev.Set("@l", level.CLEFName())
	if mt != "" {
		ev.Set("@mt", mt)
	}
	ev.Set("Source", e.cfg.Source)
	if module != "" {
		ev.Set("Module", module)
	}
	if eventID != "" {
		ev.Set("EventID", eventID)
	}
	for k, v := range fields {
		ev.Set(k, v)
	}
	if e.cfg.Bus != nil {
		e.cfg.Bus.Publish(ev)
	}
}
