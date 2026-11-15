package config

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Drafting selects the optional, explicitly authorized Pi model integration.
// Credentials belong to Pi and are never stored here.
type Drafting struct {
	Provider       string `json:"provider"`
	Model          string `json:"model"`
	Thinking       string `json:"thinking,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	MaxInputBytes  int    `json:"max_input_bytes"`
}

func parseDrafting(raw json.RawMessage) (*Drafting, error) {
	if raw == nil {
		return nil, nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, invalid("drafting must be an object")
	}
	// Raw members distinguish omitted defaults from explicit zero and null.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, invalid("drafting must be an object")
	}
	for _, v := range fields {
		if bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return nil, invalid("drafting fields cannot be null")
		}
	}
	d := &Drafting{TimeoutSeconds: 120, MaxInputBytes: 128 << 10}
	if err := decodeStrict(raw, d); err != nil {
		return nil, err
	}
	if strings.TrimSpace(d.Provider) == "" || strings.TrimSpace(d.Model) == "" || strings.ContainsAny(d.Provider+d.Model, "\x00\r\n") {
		return nil, invalid("drafting requires provider and model")
	}
	switch d.Thinking {
	case "", "off", "minimal", "low", "medium", "high", "xhigh", "max":
	default:
		return nil, invalid("invalid drafting thinking level")
	}
	if d.TimeoutSeconds < 1 || d.TimeoutSeconds > 600 {
		return nil, invalid("drafting timeout_seconds must be between 1 and 600")
	}
	if d.MaxInputBytes < 1 || d.MaxInputBytes > 1<<20 {
		return nil, invalid("drafting max_input_bytes must be between 1 and 1048576")
	}
	return d, nil
}
