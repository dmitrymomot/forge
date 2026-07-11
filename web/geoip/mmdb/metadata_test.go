package mmdb

import (
	"os"
	"testing"
)

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
