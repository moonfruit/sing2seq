// Package clef 提供 Compact Log Event Format (CLEF) 原语，以及把
// sing-box 日志解析成 CLEF 事件的解析器。
package clef

import (
	"bytes"
	"encoding/json"
)

// Event 是 CLEF 事件，字段顺序由 Set 调用顺序决定。Seq UI 按顺序展示，
// 不要随便改字段写入顺序。
type Event struct {
	keys   []string
	values map[string]any
}

// NewEvent 创建一个空事件。
func NewEvent() *Event {
	return &Event{values: map[string]any{}}
}

// Set 添加或更新一个字段；首次写入追加到 keys 末尾，重复写仅更新值不动顺序。
func (e *Event) Set(k string, v any) {
	if _, ok := e.values[k]; !ok {
		e.keys = append(e.keys, k)
	}
	e.values[k] = v
}

// Get 返回字段值；不存在时第二个返回值为 false。
func (e *Event) Get(k string) (any, bool) {
	v, ok := e.values[k]
	return v, ok
}

// Keys 返回有序键列表的副本。
func (e *Event) Keys() []string {
	out := make([]string, len(e.keys))
	copy(out, e.keys)
	return out
}

// MarshalJSON 按插入顺序序列化字段。
func (e *Event) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range e.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		vb, err := json.Marshal(e.values[k])
		if err != nil {
			return nil, err
		}
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}
