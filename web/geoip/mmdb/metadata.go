package mmdb

import (
	"bytes"
	"math"
)

var metadataMarker = []byte("\xab\xcd\xefMaxMind.com")

type database struct {
	closer func() error

	data []byte

	nodeCount  uint32
	nodeBytes  uint32 // bytes per node = recordSize/4
	treeSize   uint32 // nodeBytes * nodeCount
	dataStart  uint32 // treeSize + 16 (base for in-data pointers)
	ipv4Start  uint32 // node where IPv4 lookups begin in a v6 tree
	recordSize uint16 // 24, 28, or 32
	ipVersion  uint16 // 4 or 6
}

// parseMetadata locates and decodes the metadata section, validates it, and
// derives the tree geometry. closer is stored on the database for Close/Reload.
func parseMetadata(data []byte, closer func() error) (*database, error) {
	idx := bytes.LastIndex(data, metadataMarker)
	if idx < 0 {
		return nil, ErrInvalidDatabase
	}
	off := idx + len(metadataMarker)

	typ, count, entryOff, err := ctrl(data, off)
	if err != nil || typ != typeMap {
		return nil, ErrInvalidDatabase
	}
	off = entryOff

	db := &database{data: data, closer: closer}
	var major uint64
	for range count {
		key, next, kerr := stringAt(data, off)
		if kerr != nil {
			return nil, ErrInvalidDatabase
		}
		off = next
		switch key {
		case "node_count":
			v, n, e := uintAt(data, off)
			db.nodeCount, off, err = uint32(v), n, e
		case "record_size":
			v, n, e := uintAt(data, off)
			db.recordSize, off, err = uint16(v), n, e
		case "ip_version":
			v, n, e := uintAt(data, off)
			db.ipVersion, off, err = uint16(v), n, e
		case "binary_format_major_version":
			v, n, e := uintAt(data, off)
			major, off, err = v, n, e
		default:
			off, err = skipValue(data, off, 0)
		}
		if err != nil {
			return nil, ErrInvalidDatabase
		}
	}

	if major != 2 {
		return nil, ErrUnsupportedFormat
	}
	if db.recordSize != 24 && db.recordSize != 28 && db.recordSize != 32 {
		return nil, ErrInvalidDatabase
	}
	if db.nodeCount == 0 {
		return nil, ErrInvalidDatabase
	}
	if db.ipVersion != 4 && db.ipVersion != 6 {
		return nil, ErrInvalidDatabase
	}
	db.nodeBytes = uint32(db.recordSize) / 4
	// Compute geometry in uint64 and bounds-check before narrowing to
	// uint32: nodeBytes*nodeCount can overflow uint32 for a crafted
	// node_count, which would wrap treeSize/dataStart and defeat the
	// bounds check below, letting later node reads run out of data.
	treeSize := uint64(db.nodeBytes) * uint64(db.nodeCount)
	dataStart := treeSize + 16
	if dataStart > uint64(len(data)) {
		return nil, ErrInvalidDatabase
	}
	// Belt-and-suspenders: dataStart already passed the len(data) bound above,
	// so a >=4GB geometry can only occur with a >=4GB data slice. Guard the
	// narrowing explicitly rather than relying on that being unreachable.
	if dataStart > math.MaxUint32 {
		return nil, ErrInvalidDatabase
	}
	db.treeSize = uint32(treeSize)
	db.dataStart = uint32(dataStart)
	db.ipv4Start = db.computeIPv4Start()
	return db, nil
}

// readNode returns the left (bit==0) or right (bit==1) record value of node.
func (db *database) readNode(node uint32, bit int) uint32 {
	base := node * db.nodeBytes
	d := db.data[base : base+db.nodeBytes]
	switch db.recordSize {
	case 24:
		if bit == 0 {
			return uint32(d[0])<<16 | uint32(d[1])<<8 | uint32(d[2])
		}
		return uint32(d[3])<<16 | uint32(d[4])<<8 | uint32(d[5])
	case 28:
		if bit == 0 {
			return uint32(d[3]&0xf0)<<20 | uint32(d[0])<<16 | uint32(d[1])<<8 | uint32(d[2])
		}
		return uint32(d[3]&0x0f)<<24 | uint32(d[4])<<16 | uint32(d[5])<<8 | uint32(d[6])
	default: // 32
		if bit == 0 {
			return uint32(d[0])<<24 | uint32(d[1])<<16 | uint32(d[2])<<8 | uint32(d[3])
		}
		return uint32(d[4])<<24 | uint32(d[5])<<16 | uint32(d[6])<<8 | uint32(d[7])
	}
}

// computeIPv4Start walks 96 zero bits from the root so IPv4 lookups in a v6
// tree begin at the right node. For a v4 database it is node 0.
func (db *database) computeIPv4Start() uint32 {
	if db.ipVersion == 4 {
		return 0
	}
	node := uint32(0)
	for i := 0; i < 96 && node < db.nodeCount; i++ {
		node = db.readNode(node, 0)
	}
	return node
}
