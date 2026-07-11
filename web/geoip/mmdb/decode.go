package mmdb

const maxDepth = 32

const (
	typeExtended = 0
	typePointer  = 1
	typeString   = 2
	typeDouble   = 3
	typeBytes    = 4
	typeUint16   = 5
	typeUint32   = 6
	typeMap      = 7
	typeInt32    = 8
	typeUint64   = 9
	typeUint128  = 10
	typeArray    = 11
	typeBool     = 14
	typeFloat    = 15
)

// ctrl decodes the control byte(s) at off, returning the value type, its size
// field, and the offset of the value's data (just past the control/size bytes).
// For a pointer, size is the raw low-5-bits field (pointer decoding is done by
// database.pointer). For bool, size is the boolean value (0 or 1) and no data
// bytes follow.
func ctrl(data []byte, off int) (typ, size, dataOff int, err error) {
	if off < 0 || off >= len(data) {
		return 0, 0, 0, ErrInvalidDatabase
	}
	b := data[off]
	typ = int(b >> 5)
	off++
	if typ == typeExtended {
		if off >= len(data) {
			return 0, 0, 0, ErrInvalidDatabase
		}
		typ = int(data[off]) + 7
		off++
	}
	size = int(b & 0x1f)
	switch {
	case typ == typePointer:
		return typ, size, off, nil // pointer size handled by database.pointer
	case size < 29:
		// size is exact
	case size == 29:
		if off >= len(data) {
			return 0, 0, 0, ErrInvalidDatabase
		}
		size = 29 + int(data[off])
		off++
	case size == 30:
		if off+2 > len(data) {
			return 0, 0, 0, ErrInvalidDatabase
		}
		size = 285 + int(data[off])<<8 + int(data[off+1])
		off += 2
	default: // 31
		if off+3 > len(data) {
			return 0, 0, 0, ErrInvalidDatabase
		}
		size = 65821 + int(data[off])<<16 + int(data[off+1])<<8 + int(data[off+2])
		off += 3
	}
	return typ, size, off, nil
}

// stringAt decodes a string value at off, returning the string and the offset
// just past it.
func stringAt(data []byte, off int) (string, int, error) {
	typ, size, dataOff, err := ctrl(data, off)
	if err != nil {
		return "", 0, err
	}
	if typ != typeString && typ != typeBytes {
		return "", 0, ErrInvalidDatabase
	}
	if size < 0 || dataOff+size > len(data) {
		return "", 0, ErrInvalidDatabase
	}
	return string(data[dataOff : dataOff+size]), dataOff + size, nil
}

// uintAt decodes an unsigned integer value (uint16/uint32/uint64) at off.
func uintAt(data []byte, off int) (uint64, int, error) {
	typ, size, dataOff, err := ctrl(data, off)
	if err != nil {
		return 0, 0, err
	}
	if typ != typeUint16 && typ != typeUint32 && typ != typeUint64 {
		return 0, 0, ErrInvalidDatabase
	}
	if size > 8 || dataOff+size > len(data) {
		return 0, 0, ErrInvalidDatabase
	}
	var v uint64
	for i := range size {
		v = v<<8 | uint64(data[dataOff+i])
	}
	return v, dataOff + size, nil
}

// skipValue advances past the value at off (following the format's structure,
// but NOT following pointers — a pointer's own bytes are skipped in situ).
// depth guards against pointer-cycle / deeply nested corruption.
func skipValue(data []byte, off, depth int) (int, error) {
	if depth > maxDepth {
		return 0, ErrInvalidDatabase
	}
	typ, size, dataOff, err := ctrl(data, off)
	if err != nil {
		return 0, err
	}
	switch typ {
	case typePointer:
		ptrSize := (int(data[off]) >> 3) & 0x3
		return dataOff + ptrSize + 1, boundsErr(dataOff+ptrSize+1, len(data))
	case typeString, typeBytes:
		return dataOff + size, boundsErr(dataOff+size, len(data))
	case typeDouble:
		return dataOff + 8, boundsErr(dataOff+8, len(data))
	case typeFloat:
		return dataOff + 4, boundsErr(dataOff+4, len(data))
	case typeUint16, typeUint32, typeUint64, typeUint128, typeInt32:
		return dataOff + size, boundsErr(dataOff+size, len(data))
	case typeBool:
		return dataOff, nil // bool stores its value in size; no data bytes
	case typeMap:
		o := dataOff
		for range size {
			if o, err = skipValue(data, o, depth+1); err != nil { // key
				return 0, err
			}
			if o, err = skipValue(data, o, depth+1); err != nil { // value
				return 0, err
			}
		}
		return o, nil
	case typeArray:
		o := dataOff
		for range size {
			if o, err = skipValue(data, o, depth+1); err != nil {
				return 0, err
			}
		}
		return o, nil
	default:
		return 0, ErrInvalidDatabase
	}
}

func boundsErr(end, n int) error {
	if end > n {
		return ErrInvalidDatabase
	}
	return nil
}
