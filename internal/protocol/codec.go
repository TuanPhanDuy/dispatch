package protocol

import (
	"bufio"
	"encoding/json"
)

// WriteEnvelope marshals env as one JSON line and flushes it immediately —
// each call is a complete, self-contained write so concurrent callers must
// still serialize access to w themselves (protocol doesn't own locking).
func WriteEnvelope(w *bufio.Writer, env Envelope) error {
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	if err := w.WriteByte('\n'); err != nil {
		return err
	}
	return w.Flush()
}

// ReadEnvelope reads one newline-terminated JSON line. Using ReadBytes
// instead of bufio.Scanner avoids Scanner's fixed max-token-size limit,
// since PTY output bursts can produce large lines.
func ReadEnvelope(r *bufio.Reader) (Envelope, error) {
	line, err := r.ReadBytes('\n')
	if err != nil {
		return Envelope{}, err
	}
	var env Envelope
	if err := json.Unmarshal(line, &env); err != nil {
		return Envelope{}, err
	}
	return env, nil
}

// Marshal is a convenience wrapper for building an Envelope's Data field.
func Marshal(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	data, _ := json.Marshal(v)
	return data
}

// Unmarshal is a convenience wrapper for decoding an Envelope's Data field.
func Unmarshal(data json.RawMessage, v any) error {
	return json.Unmarshal(data, v)
}
