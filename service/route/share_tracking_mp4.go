package route

import (
	"encoding/binary"
	"errors"
)

// Read dimensions from bounded ISO BMFF metadata, without executing media or external programs.
func trackingMP4Dimensions(data []byte) (int, int, error) {
	var width, height int
	var walk func([]byte, int) error
	walk = func(b []byte, depth int) error {
		if depth > 4 {
			return errors.New("mp4 nesting too deep")
		}
		for len(b) >= 8 {
			size := uint64(binary.BigEndian.Uint32(b[:4]))
			kind := string(b[4:8])
			header := uint64(8)
			if size == 1 {
				if len(b) < 16 {
					return errors.New("short atom")
				}
				size = binary.BigEndian.Uint64(b[8:16])
				header = 16
			}
			if size == 0 {
				size = uint64(len(b))
			}
			if size < header || size > uint64(len(b)) {
				return errors.New("invalid atom")
			}
			payload := b[header:size]
			switch kind {
			case "moov", "trak":
				if e := walk(payload, depth+1); e != nil {
					return e
				}
			case "tkhd":
				if len(payload) >= 84 {
					w := int(binary.BigEndian.Uint32(payload[len(payload)-8:len(payload)-4]) >> 16)
					h := int(binary.BigEndian.Uint32(payload[len(payload)-4:]) >> 16)
					if w > 0 && h > 0 {
						matrix := payload[len(payload)-44 : len(payload)-8]
						a := int32(binary.BigEndian.Uint32(matrix[0:4]))
						bb := int32(binary.BigEndian.Uint32(matrix[4:8]))
						cc := int32(binary.BigEndian.Uint32(matrix[12:16]))
						d := int32(binary.BigEndian.Uint32(matrix[16:20]))
						if a == 0 && d == 0 && bb != 0 && cc != 0 {
							w, h = h, w
						}
						width, height = w, h
					}
				}
			}
			b = b[size:]
		}
		return nil
	}
	if e := walk(data, 0); e != nil {
		return 0, 0, e
	}
	if width < 1 || height < 1 || width > 20000 || height > 20000 {
		return 0, 0, errors.New("missing video dimensions")
	}
	return width, height, nil
}
