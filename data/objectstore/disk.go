package objectstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/dmitrymomot/forge/core/filetype"
)

// tmpDir is Disk's reserved top-level directory for in-flight writes. Keys
// under it are rejected and List never yields it.
const tmpDir = ".tmp"

// Disk is a path-traversal-safe Store on a local directory. Every access
// goes through an os.Root, so neither crafted keys nor symlinks planted
// inside the directory can reach files outside it; keys are additionally
// validated and localized (filepath.Localize) before use.
//
// Writes are atomic: content streams to a temp file under a reserved ".tmp"
// directory, is fsynced, and is renamed into place — readers never observe a
// partial object. Content types are not persisted; Get and Stat re-detect
// them from the object's leading bytes. Deleting an object leaves its parent
// directories behind.
type Disk struct {
	root *os.Root
}

// NewDisk opens (creating if needed) dir and returns a Disk rooted there.
// Close releases the root when the store is no longer needed.
func NewDisk(dir string) (*Disk, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("objectstore: create root: %w", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("objectstore: open root: %w", err)
	}
	return &Disk{root: root}, nil
}

// Close releases the underlying directory handle. The Disk is unusable
// afterwards.
func (d *Disk) Close() error { return d.root.Close() }

// localize validates key and converts it to a safe OS path under the root.
func (d *Disk) localize(key string) (string, error) {
	if err := ValidateKey(key); err != nil {
		return "", err
	}
	if key == tmpDir || strings.HasPrefix(key, tmpDir+"/") {
		return "", fmt.Errorf("%w: %q is reserved", ErrInvalidKey, tmpDir)
	}
	local, err := filepath.Localize(key)
	if err != nil {
		return "", fmt.Errorf("%w: %q: %w", ErrInvalidKey, key, err)
	}
	return local, nil
}

// Put streams r to a temp file and atomically renames it over key,
// replacing any existing object. contentType is ignored — Disk re-detects
// types on read.
func (d *Disk) Put(ctx context.Context, key, _ string, r io.Reader) error {
	local, err := d.localize(key)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := d.root.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("objectstore: create temp dir: %w", err)
	}
	var suffix [16]byte
	rand.Read(suffix[:])
	tmp := filepath.Join(tmpDir, "put-"+hex.EncodeToString(suffix[:]))
	f, err := d.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("objectstore: create temp file: %w", err)
	}
	if err := writeAndSync(f, r); err != nil {
		_ = f.Close()
		_ = d.root.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = d.root.Remove(tmp)
		return fmt.Errorf("objectstore: close temp file: %w", err)
	}
	if dir := path.Dir(key); dir != "." {
		localDir, lerr := filepath.Localize(dir)
		if lerr != nil {
			_ = d.root.Remove(tmp)
			return fmt.Errorf("%w: %q: %w", ErrInvalidKey, key, lerr)
		}
		if err := d.root.MkdirAll(localDir, 0o755); err != nil {
			_ = d.root.Remove(tmp)
			return fmt.Errorf("objectstore: create parent dirs: %w", err)
		}
	}
	if err := d.root.Rename(tmp, local); err != nil {
		_ = d.root.Remove(tmp)
		return fmt.Errorf("objectstore: rename into place: %w", err)
	}
	return nil
}

// writeAndSync copies r to f and flushes it to stable storage, so the
// subsequent rename never publishes an object that a crash could truncate.
func writeAndSync(f *os.File, r io.Reader) error {
	if _, err := io.Copy(f, r); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("objectstore: sync temp file: %w", err)
	}
	return nil
}

// Get opens the object and returns its content and Info; the content type is
// detected from the leading bytes.
func (d *Disk) Get(ctx context.Context, key string) (io.ReadCloser, Info, error) {
	local, err := d.localize(key)
	if err != nil {
		return nil, Info{}, err
	}
	if err := ctx.Err(); err != nil {
		return nil, Info{}, err
	}
	f, err := d.root.Open(local)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, Info{}, fmt.Errorf("%w: %q", ErrNotFound, key)
		}
		return nil, Info{}, fmt.Errorf("objectstore: open: %w", err)
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, Info{}, fmt.Errorf("objectstore: stat: %w", err)
	}
	if fi.IsDir() {
		_ = f.Close()
		return nil, Info{}, fmt.Errorf("%w: %q", ErrNotFound, key)
	}
	typ, body, err := filetype.DetectReader(f)
	if err != nil {
		_ = f.Close()
		return nil, Info{}, fmt.Errorf("objectstore: detect type: %w", err)
	}
	info := Info{Key: key, ContentType: typ.MIME, Size: fi.Size(), ModTime: fi.ModTime()}
	return &diskObject{Reader: body, file: f}, info, nil
}

// diskObject replays the sniffed head followed by the rest of the file and
// closes the underlying handle.
type diskObject struct {
	io.Reader
	file *os.File
}

func (o *diskObject) Close() error { return o.file.Close() }

// Stat returns the object's Info; the content type is detected from the
// leading bytes.
func (d *Disk) Stat(ctx context.Context, key string) (Info, error) {
	rc, info, err := d.Get(ctx, key)
	if err != nil {
		return Info{}, err
	}
	_ = rc.Close()
	return info, nil
}

// Delete removes the object; deleting an absent key is not an error. Parent
// directories are left behind.
func (d *Disk) Delete(ctx context.Context, key string) error {
	local, err := d.localize(key)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	fi, err := d.root.Lstat(local)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("objectstore: stat: %w", err)
	}
	if fi.IsDir() {
		// A directory is not an object; the key has nothing to delete.
		return nil
	}
	if err := d.root.Remove(local); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("objectstore: remove: %w", err)
	}
	return nil
}

// List yields regular files whose key starts with prefix, walking only the
// directory the prefix pins down. Order is the directory walk's lexical
// order (which differs from pure key order when directories and files mix).
// Symlinks and the reserved ".tmp" directory are skipped.
func (d *Disk) List(ctx context.Context, prefix string) iter.Seq2[Info, error] {
	return func(yield func(Info, error) bool) {
		if err := ValidatePrefix(prefix); err != nil {
			yield(Info{}, err)
			return
		}
		if strings.HasPrefix(prefix, tmpDir+"/") {
			return // only .tmp internals can match — never objects
		}
		start := "."
		if i := strings.LastIndexByte(prefix, '/'); i > 0 {
			start = prefix[:i]
		}
		walkErr := fs.WalkDir(d.root.FS(), start, func(p string, entry fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return fs.SkipAll // nothing under the prefix
				}
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if entry.IsDir() {
				if p == tmpDir {
					return fs.SkipDir
				}
				// Descend only into directories that can hold matching keys.
				if p != "." && !strings.HasPrefix(p, prefix) && !strings.HasPrefix(prefix, p+"/") {
					return fs.SkipDir
				}
				return nil
			}
			if !entry.Type().IsRegular() || !strings.HasPrefix(p, prefix) {
				return nil
			}
			fi, err := entry.Info()
			if err != nil {
				return err
			}
			if !yield(Info{Key: p, Size: fi.Size(), ModTime: fi.ModTime()}, nil) {
				return fs.SkipAll
			}
			return nil
		})
		if walkErr != nil {
			yield(Info{}, fmt.Errorf("objectstore: list: %w", walkErr))
		}
	}
}
