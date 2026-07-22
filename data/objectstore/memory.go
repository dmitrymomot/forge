package objectstore

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"iter"
	"slices"
	"strings"
	"sync"
	"time"
)

// Memory is an in-process Store for tests and development. Objects live in a
// map guarded by a mutex; List yields in lexicographic key order (matching
// S3). Not for production data — everything is lost on process exit.
type Memory struct {
	objects map[string]memObject
	mu      sync.RWMutex
}

type memObject struct {
	modTime     time.Time
	contentType string
	data        []byte
}

// NewMemory returns an empty in-memory Store.
func NewMemory() *Memory {
	return &Memory{objects: make(map[string]memObject)}
}

// Put stores r's content under key, replacing any existing object.
func (m *Memory) Put(ctx context.Context, key, contentType string, r io.Reader) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	m.objects[key] = memObject{data: data, contentType: contentType, modTime: time.Now()}
	m.mu.Unlock()
	return nil
}

// Get returns the stored object's content and Info.
func (m *Memory) Get(ctx context.Context, key string) (io.ReadCloser, Info, error) {
	info, data, err := m.lookup(ctx, key)
	if err != nil {
		return nil, Info{}, err
	}
	return io.NopCloser(bytes.NewReader(data)), info, nil
}

// Stat returns the stored object's Info.
func (m *Memory) Stat(ctx context.Context, key string) (Info, error) {
	info, _, err := m.lookup(ctx, key)
	return info, err
}

func (m *Memory) lookup(ctx context.Context, key string) (Info, []byte, error) {
	if err := ValidateKey(key); err != nil {
		return Info{}, nil, err
	}
	if err := ctx.Err(); err != nil {
		return Info{}, nil, err
	}
	m.mu.RLock()
	obj, ok := m.objects[key]
	m.mu.RUnlock()
	if !ok {
		return Info{}, nil, fmt.Errorf("%w: %q", ErrNotFound, key)
	}
	return Info{Key: key, ContentType: obj.contentType, Size: int64(len(obj.data)), ModTime: obj.modTime}, obj.data, nil
}

// Delete removes the object; deleting an absent key is not an error.
func (m *Memory) Delete(ctx context.Context, key string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.objects, key)
	m.mu.Unlock()
	return nil
}

// List yields objects whose key starts with prefix in lexicographic order.
// The listing is a point-in-time snapshot: mutations during iteration are
// not reflected.
func (m *Memory) List(ctx context.Context, prefix string) iter.Seq2[Info, error] {
	return func(yield func(Info, error) bool) {
		if err := ValidatePrefix(prefix); err != nil {
			yield(Info{}, err)
			return
		}
		if err := ctx.Err(); err != nil {
			yield(Info{}, err)
			return
		}
		m.mu.RLock()
		infos := make([]Info, 0, len(m.objects))
		for key, obj := range m.objects {
			if strings.HasPrefix(key, prefix) {
				infos = append(infos, Info{Key: key, Size: int64(len(obj.data)), ModTime: obj.modTime})
			}
		}
		m.mu.RUnlock()
		slices.SortFunc(infos, func(a, b Info) int { return strings.Compare(a.Key, b.Key) })
		for _, info := range infos {
			if !yield(info, nil) {
				return
			}
		}
	}
}
