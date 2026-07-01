package mapx

import (
	"bytes"
	"encoding"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"strconv"
)

// Ordered is a map that remembers insertion order and marshals to JSON in that
// order. Updating an existing key keeps its position; deleting removes it from
// the order; re-adding appends at the end.
//
// Always hold and pass *Ordered[K, V] (NewOrdered returns one), never a value.
// MarshalJSON/UnmarshalJSON have pointer receivers, so a value embedded in a
// struct and marshaled non-pointer would NOT call them — encoding/json would
// reflect over the unexported fields and silently emit {} instead.
type Ordered[K comparable, V any] struct {
	m    map[K]V
	keys []K
}

// NewOrdered returns an empty ordered map ready for use.
func NewOrdered[K comparable, V any]() *Ordered[K, V] {
	return &Ordered[K, V]{m: make(map[K]V)}
}

func (o *Ordered[K, V]) ensure() {
	if o.m == nil {
		o.m = make(map[K]V)
	}
}

// Set inserts or updates k. Insertion appends to the key order; update keeps
// the existing position.
func (o *Ordered[K, V]) Set(k K, v V) {
	o.ensure()
	if _, ok := o.m[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.m[k] = v
}

// Get returns the value for k and whether it is present.
func (o *Ordered[K, V]) Get(k K) (V, bool) {
	v, ok := o.m[k]
	return v, ok
}

// Delete removes k, preserving the order of the remaining keys.
func (o *Ordered[K, V]) Delete(k K) {
	if _, ok := o.m[k]; !ok {
		return
	}
	delete(o.m, k)
	for i, kk := range o.keys {
		if kk == k {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			return
		}
	}
}

// Len returns the number of entries.
func (o *Ordered[K, V]) Len() int { return len(o.keys) }

// Keys returns a copy of the keys in insertion order.
func (o *Ordered[K, V]) Keys() []K {
	out := make([]K, len(o.keys))
	copy(out, o.keys)
	return out
}

// All iterates entries in insertion order.
func (o *Ordered[K, V]) All() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for _, k := range o.keys {
			if !yield(k, o.m[k]) {
				return
			}
		}
	}
}

// MarshalJSON emits a JSON object with keys in insertion order.
func (o *Ordered[K, V]) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, err := marshalKey(k)
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		vb, err := json.Marshal(o.m[k])
		if err != nil {
			return nil, err
		}
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// UnmarshalJSON decodes a JSON object, preserving source key order. A JSON null
// is a no-op.
func (o *Ordered[K, V]) UnmarshalJSON(b []byte) error {
	o.ensure()
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if tok == nil {
		return nil // JSON null
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("mapx: cannot unmarshal into Ordered: expected JSON object")
	}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return err
		}
		ks, ok := kt.(string)
		if !ok {
			return fmt.Errorf("mapx: non-string object key")
		}
		var k K
		if err := unmarshalKey(ks, &k); err != nil {
			return err
		}
		var v V
		if err := dec.Decode(&v); err != nil {
			return err
		}
		o.Set(k, v)
	}
	if _, err := dec.Token(); err != nil { // consume closing '}'
		return err
	}
	if _, err := dec.Token(); err != io.EOF { // reject trailing data after the object
		return fmt.Errorf("mapx: unexpected trailing data after JSON object")
	}
	return nil
}

// marshalKey encodes a map key as a JSON string, mirroring encoding/json's map
// key rules (string, encoding.TextMarshaler, and integer kinds as quoted
// numbers).
func marshalKey[K comparable](k K) ([]byte, error) {
	switch kv := any(k).(type) {
	case string:
		return json.Marshal(kv)
	case encoding.TextMarshaler:
		t, err := kv.MarshalText()
		if err != nil {
			return nil, err
		}
		return json.Marshal(string(t))
	default:
		b, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		if len(b) > 0 && b[0] == '"' {
			return b, nil
		}
		return json.Marshal(string(b)) // quote numeric keys, e.g. 123 -> "123"
	}
}

// unmarshalKey decodes a JSON object key string into K, mirroring marshalKey.
func unmarshalKey[K comparable](s string, k *K) error {
	switch kp := any(k).(type) {
	case *string:
		*kp = s
		return nil
	case encoding.TextUnmarshaler:
		return kp.UnmarshalText([]byte(s))
	default:
		if err := json.Unmarshal([]byte(s), k); err == nil {
			return nil
		}
		return json.Unmarshal([]byte(strconv.Quote(s)), k)
	}
}
