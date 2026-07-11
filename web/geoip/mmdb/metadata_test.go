package mmdb

import (
	"errors"
	"os"
	"testing"
)

// encodeCtrl builds a MaxMind-DB control byte sequence for typ/size, using the
// exact-size encoding (size < 29) which is all these tests need.
func encodeCtrl(typ, size int) []byte {
	if size < 0 || size >= 29 {
		panic("encodeCtrl: size out of exact-size range")
	}
	return []byte{byte(typ<<5 | size)}
}

// encodeString hand-encodes a data-section string value.
func encodeString(s string) []byte {
	return append(encodeCtrl(typeString, len(s)), []byte(s)...)
}

// encodeUint hand-encodes a data-section unsigned-integer value using the
// minimal number of big-endian bytes, tagged with the given type (uint16,
// uint32, or uint64 all decode identically in uintAt).
func encodeUint(typ int, v uint64) []byte {
	b := []byte{}
	for n := v; n > 0; n >>= 8 {
		b = append([]byte{byte(n)}, b...)
	}
	return append(encodeCtrl(typ, len(b)), b...)
}

// buildMetadata hand-encodes a minimal metadata map (4 entries: node_count,
// record_size, ip_version, binary_format_major_version) as it appears in the
// MaxMind-DB data section, for use in parseMetadata regression tests.
func buildMetadata(nodeCount uint64, recordSize, ipVersion, major uint16) []byte {
	buf := encodeCtrl(typeMap, 4)
	buf = append(buf, encodeString("node_count")...)
	buf = append(buf, encodeUint(typeUint32, nodeCount)...)
	buf = append(buf, encodeString("record_size")...)
	buf = append(buf, encodeUint(typeUint16, uint64(recordSize))...)
	buf = append(buf, encodeString("ip_version")...)
	buf = append(buf, encodeUint(typeUint16, uint64(ipVersion))...)
	buf = append(buf, encodeString("binary_format_major_version")...)
	buf = append(buf, encodeUint(typeUint16, uint64(major))...)
	return buf
}

// TestParseMetadataRejectsOverflowNodeCount guards against a crafted metadata
// section where nodeBytes*nodeCount overflows uint32. record_size=32 gives
// nodeBytes=8; node_count=2^29 makes 8*2^29 wrap to exactly 0 mod 2^32, so a
// naive uint32 bounds check on treeSize/dataStart would pass on a tiny file
// and readNode would then index out of the (short) data slice and panic.
func TestParseMetadataRejectsOverflowNodeCount(t *testing.T) {
	data := append(append([]byte{}, metadataMarker...), buildMetadata(1<<29, 32, 6, 2)...)

	db, err := parseMetadata(data, func() error { return nil })
	if err == nil {
		t.Fatalf("expected error for overflowing node_count, got db = %+v", db)
	}
	if !errors.Is(err, ErrInvalidDatabase) {
		t.Fatalf("err = %v, want ErrInvalidDatabase", err)
	}
}

// TestParseMetadataAcceptsValidSmallMetadata proves buildMetadata's hand
// encoding round-trips correctly: a small, non-overflowing metadata section
// must parse without error.
func TestParseMetadataAcceptsValidSmallMetadata(t *testing.T) {
	data := append(append([]byte{}, metadataMarker...), buildMetadata(1, 24, 6, 2)...)

	db, err := parseMetadata(data, func() error { return nil })
	if err != nil {
		t.Fatalf("parseMetadata failed on valid small metadata: %v", err)
	}
	if db.nodeCount != 1 || db.recordSize != 24 || db.ipVersion != 6 {
		t.Fatalf("unexpected db fields: %+v", db)
	}
}

func loadDB(t *testing.T, name string) *database {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	db, err := parseMetadata(data, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestParseMetadataCity(t *testing.T) {
	db := loadDB(t, "GeoIP2-City-Test.mmdb")
	if db.nodeCount == 0 {
		t.Fatal("nodeCount should be > 0")
	}
	if db.recordSize != 24 && db.recordSize != 28 && db.recordSize != 32 {
		t.Fatalf("recordSize = %d", db.recordSize)
	}
	if db.ipVersion != 6 {
		t.Fatalf("ipVersion = %d, want 6", db.ipVersion)
	}
	if int(db.dataStart) > len(db.data) {
		t.Fatal("dataStart past end")
	}
	if db.nodeBytes != uint32(db.recordSize)/4 {
		t.Fatalf("nodeBytes = %d", db.nodeBytes)
	}
}

func TestParseMetadataRejectsGarbage(t *testing.T) {
	if _, err := parseMetadata([]byte("not a database"), nil); err == nil {
		t.Fatal("garbage should fail to parse")
	}
}
