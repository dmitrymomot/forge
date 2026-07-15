package queue

import (
	"encoding/json"
	"fmt"
	"time"
)

// wireVersion is the envelope encoding version. Bump only with a decoder
// that still accepts every previous version: dead-lettered jobs encoded by
// old binaries must stay requeueable.
const wireVersion = 1

type wireJob struct {
	ID          string          `json:"id"`
	Queue       string          `json:"q"`
	Type        string          `json:"t"`
	Scope       string          `json:"s,omitempty"`
	LastError   string          `json:"le,omitempty"`
	Payload     json.RawMessage `json:"p,omitempty"`
	V           int             `json:"v"`
	Attempt     int             `json:"a,omitempty"`
	MaxAttempts int             `json:"ma,omitempty"`
	RunAtMS     int64           `json:"ra,omitempty"`
	CreatedAtMS int64           `json:"ca,omitempty"`
}

// EncodeJob serializes a Job into the stable, versioned wire form used by
// non-columnar brokers and DLQ storage.
func EncodeJob(j Job) ([]byte, error) {
	w := wireJob{
		V: wireVersion, ID: j.ID, Queue: j.Queue, Type: j.Type,
		Payload: json.RawMessage(j.Payload), Scope: j.Scope,
		Attempt: j.Attempt, MaxAttempts: j.MaxAttempts, LastError: j.LastError,
	}
	if !j.RunAt.IsZero() {
		w.RunAtMS = j.RunAt.UnixMilli()
	}
	if !j.CreatedAt.IsZero() {
		w.CreatedAtMS = j.CreatedAt.UnixMilli()
	}
	b, err := json.Marshal(w)
	if err != nil {
		return nil, fmt.Errorf("queue: encode job: %w", err)
	}
	return b, nil
}

// DecodeJob parses the wire form produced by EncodeJob.
func DecodeJob(b []byte) (Job, error) {
	var w wireJob
	if err := json.Unmarshal(b, &w); err != nil {
		return Job{}, fmt.Errorf("queue: decode job: %w", err)
	}
	if w.V != wireVersion {
		return Job{}, fmt.Errorf("queue: decode job: unsupported wire version %d", w.V)
	}
	j := Job{
		ID: w.ID, Queue: w.Queue, Type: w.Type, Payload: []byte(w.Payload),
		Scope: w.Scope, Attempt: w.Attempt, MaxAttempts: w.MaxAttempts, LastError: w.LastError,
	}
	if w.RunAtMS != 0 {
		j.RunAt = time.UnixMilli(w.RunAtMS).UTC()
	}
	if w.CreatedAtMS != 0 {
		j.CreatedAt = time.UnixMilli(w.CreatedAtMS).UTC()
	}
	return j, nil
}
