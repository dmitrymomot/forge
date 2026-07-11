package mmdb

import (
	"context"
	"net/netip"
	"sync"

	"github.com/dmitrymomot/forge/web/geoip"
)

// Reader is a MaxMind-DB-format geo database. It implements geoip.Locator and
// is safe for concurrent use; Reload atomically swaps the underlying data.
type Reader struct {
	city *database
	asn  *database
	mu   sync.RWMutex
}

// New opens the configured databases. It returns ErrNoDatabase if neither a
// city nor an ASN database was provided.
func New(opts ...Option) (*Reader, error) {
	c := newConfig(opts...)
	r := &Reader{}
	if err := r.load(c); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Reader) load(c config) error {
	city, err := openDB(c.city, c.inMemory)
	if err != nil {
		return err
	}
	asn, err := openDB(c.asn, c.inMemory)
	if err != nil {
		closeDB(city)
		return err
	}
	if city == nil && asn == nil {
		return ErrNoDatabase
	}
	r.mu.Lock()
	old := []*database{r.city, r.asn}
	r.city, r.asn = city, asn
	r.mu.Unlock()
	for _, d := range old {
		closeDB(d)
	}
	return nil
}

// openDB loads one optional source into a parsed database. A source with
// neither path nor bytes yields (nil, nil) so an absent DB is not an error.
func openDB(src source, inMemory bool) (*database, error) {
	if !src.hasPath && src.bytes == nil {
		return nil, nil
	}
	data, closer, err := loadSource(src, inMemory)
	if err != nil {
		return nil, err
	}
	db, err := parseMetadata(data, closer)
	if err != nil {
		_ = closer()
		return nil, err
	}
	return db, nil
}

func closeDB(db *database) {
	if db != nil && db.closer != nil {
		_ = db.closer()
	}
}

// Close unmaps every open database. The Reader must not be used afterwards.
func (r *Reader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	closeDB(r.city)
	closeDB(r.asn)
	r.city, r.asn = nil, nil
	return nil
}

// lookupOffset walks the search tree for ip and returns the data-section
// offset of its record, or (0, false) on a miss.
func (db *database) lookupOffset(ip netip.Addr) (int, bool) {
	ip = ip.Unmap()
	var node uint32
	var bits []byte
	if ip.Is4() {
		node = db.ipv4Start
		b := ip.As4()
		bits = b[:]
	} else {
		if db.ipVersion == 4 {
			return 0, false // IPv6 address, IPv4-only DB
		}
		b := ip.As16()
		bits = b[:]
	}
	total := len(bits) * 8
	for i := range total {
		if node >= db.nodeCount {
			break
		}
		bit := int((bits[i>>3] >> (7 - uint(i&7))) & 1)
		node = db.readNode(node, bit)
	}
	if node == db.nodeCount {
		return 0, false // empty record
	}
	if node < db.nodeCount {
		return 0, false // ran out of bits inside the tree (should not happen)
	}
	off := int(node) - int(db.nodeCount) + int(db.treeSize)
	if off < int(db.dataStart) || off >= len(db.data) {
		return 0, false
	}
	return off, true
}

// pointer decodes an in-data pointer at off, returning the target offset (into
// the data section) and the offset just past the pointer in the current stream.
func (db *database) pointer(off int) (target, next int, err error) {
	if off >= len(db.data) {
		return 0, 0, ErrInvalidDatabase
	}
	b := db.data[off]
	ptrSize := int((b >> 3) & 0x3)
	v := int(b & 0x7)
	off++
	need := ptrSize + 1
	if off+need > len(db.data) {
		return 0, 0, ErrInvalidDatabase
	}
	switch ptrSize {
	case 0:
		v = v<<8 | int(db.data[off])
	case 1:
		v = (v<<16 | int(db.data[off])<<8 | int(db.data[off+1])) + 2048
	case 2:
		v = (v<<24 | int(db.data[off])<<16 | int(db.data[off+1])<<8 | int(db.data[off+2])) + 526336
	default: // 3
		v = int(db.data[off])<<24 | int(db.data[off+1])<<16 | int(db.data[off+2])<<8 | int(db.data[off+3])
	}
	target = int(db.dataStart) + v
	if target < 0 || target >= len(db.data) {
		return 0, 0, ErrInvalidDatabase
	}
	return target, off + need, nil
}

// value returns the offset to decode a value's content (following one pointer
// if present) and the offset just past the value in the current stream.
func (db *database) value(off int) (content, next int, err error) {
	typ, _, _, err := ctrl(db.data, off)
	if err != nil {
		return 0, 0, err
	}
	if typ == typePointer {
		target, n, perr := db.pointer(off)
		return target, n, perr
	}
	n, err := skipValue(db.data, off, 0)
	return off, n, err
}

// keyAt decodes a map key (possibly stored as a pointer), returning the key and
// the offset just past it in the current stream.
func (db *database) keyAt(off int) (string, int, error) {
	typ, _, _, err := ctrl(db.data, off)
	if err != nil {
		return "", 0, err
	}
	if typ == typePointer {
		target, next, perr := db.pointer(off)
		if perr != nil {
			return "", 0, perr
		}
		s, _, serr := stringAt(db.data, target)
		return s, next, serr
	}
	return stringAt(db.data, off)
}

// mapAt resolves off (following a pointer) to a map, returning the offset of its
// first entry and the entry count.
func (db *database) mapAt(off int) (entryOff, count int, err error) {
	typ, size, dataOff, err := ctrl(db.data, off)
	if err != nil {
		return 0, 0, err
	}
	if typ == typePointer {
		target, _, perr := db.pointer(off)
		if perr != nil {
			return 0, 0, perr
		}
		return db.mapAt(target)
	}
	if typ != typeMap {
		return 0, 0, ErrInvalidDatabase
	}
	return dataOff, size, nil
}

// walkMap iterates the map at off, calling fn(key, valueOff) for each entry.
func (db *database) walkMap(off int, fn func(key string, valueOff int) error) error {
	entryOff, count, err := db.mapAt(off)
	if err != nil {
		return err
	}
	o := entryOff
	for range count {
		key, next, kerr := db.keyAt(o)
		if kerr != nil {
			return kerr
		}
		o = next
		if err := fn(key, o); err != nil {
			return err
		}
		_, n, verr := db.value(o)
		if verr != nil {
			return verr
		}
		o = n
	}
	return nil
}

// firstOfArray resolves off to an array and calls fn with the offset of its
// first element (if any).
func (db *database) firstOfArray(off int, fn func(elemOff int) error) error {
	typ, size, dataOff, err := ctrl(db.data, off)
	if err != nil {
		return err
	}
	if typ == typePointer {
		target, _, perr := db.pointer(off)
		if perr != nil {
			return perr
		}
		return db.firstOfArray(target, fn)
	}
	if typ != typeArray || size == 0 {
		return nil
	}
	return fn(dataOff)
}

// stringField decodes a (possibly pointer-indirected) string value at off.
func (db *database) stringField(off int) (string, error) {
	content, _, err := db.value(off)
	if err != nil {
		return "", err
	}
	s, _, err := stringAt(db.data, content)
	return s, err
}

// uintField decodes a (possibly pointer-indirected) uint value at off.
func (db *database) uintField(off int) (uint64, error) {
	content, _, err := db.value(off)
	if err != nil {
		return 0, err
	}
	v, _, err := uintAt(db.data, content)
	return v, err
}

// enName reads the "en" entry of a names map at off into dst.
func (db *database) enName(off int, dst *string) error {
	return db.walkMap(off, func(k string, o int) error {
		if k == "en" {
			s, err := db.stringField(o)
			if err != nil {
				return err
			}
			*dst = s
		}
		return nil
	})
}

var _ geoip.Locator = (*Reader)(nil)

// Lookup is implemented in Task 10.
func (r *Reader) Lookup(ctx context.Context, ip netip.Addr) (geoip.Location, error) {
	return geoip.Location{}, nil
}
