// Package bytesize parses and formats human byte sizes and provides a ByteSize
// type that drops into env-tagged config and JSON via TextMarshaler. SI
// suffixes (KB/MB/GB...) are powers of 1000; IEC suffixes (KiB/MiB/GiB...) are
// powers of 1024. Formatting defaults to IEC and always round-trips through
// Parse (values not exact in any unit fall back to a byte count). No bit units.
//
// # Usage
//
//	n, err := bytesize.Parse("10MB")
//	if err != nil {
//		// handle invalid size
//	}
//	n.String()           // "10000000B" (not exact in IEC units, falls back to bytes)
//	bytesize.FormatSI(n) // "10MB"
package bytesize
