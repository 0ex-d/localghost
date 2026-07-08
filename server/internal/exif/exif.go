// Package exif is a minimal EXIF reader for JPEG files. It extracts exactly the two things
// ghost.framed needs , when the photo was taken (DateTimeOriginal) and where (GPS lat/lon) , and
// nothing else. Written from the TIFF/EXIF spec directly so the box carries no third-party image
// metadata dependency; a full EXIF library would be 50x the code for fields we never read.
//
// It never modifies bytes and tolerates missing or malformed EXIF: a photo without GPS or a corrupt
// APP1 segment yields zero values, not an error , the pipeline archives the photo either way.
package exif

import (
	"encoding/binary"
	"errors"
	"time"
)

// Meta is what framed needs from a photo.
type Meta struct {
	TakenAt time.Time // zero if absent/unparseable
	Lat     float64
	Lon     float64
	HasGPS  bool
}

var errNoExif = errors.New("no exif")

// Parse extracts Meta from JPEG bytes. Any structural problem returns zero Meta and nil error ,
// absence of metadata is a normal condition, not a failure.
func Parse(b []byte) Meta {
	m, err := parse(b)
	if err != nil {
		return Meta{}
	}
	return m
}

func parse(b []byte) (Meta, error) {
	// JPEG: SOI then segments. Find APP1 with "Exif\x00\x00".
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return Meta{}, errNoExif
	}
	i := 2
	var tiff []byte
	for i+4 <= len(b) {
		if b[i] != 0xFF {
			return Meta{}, errNoExif
		}
		marker := b[i+1]
		if marker == 0xD8 || (marker >= 0xD0 && marker <= 0xD9) { // no-length markers
			i += 2
			continue
		}
		if i+4 > len(b) {
			return Meta{}, errNoExif
		}
		segLen := int(binary.BigEndian.Uint16(b[i+2 : i+4]))
		if segLen < 2 || i+2+segLen > len(b) {
			return Meta{}, errNoExif
		}
		if marker == 0xE1 && segLen >= 8 { // APP1
			payload := b[i+4 : i+2+segLen]
			if len(payload) >= 6 && string(payload[:6]) == "Exif\x00\x00" {
				tiff = payload[6:]
				break
			}
		}
		if marker == 0xDA { // start of scan , no metadata past here
			break
		}
		i += 2 + segLen
	}
	if tiff == nil {
		return Meta{}, errNoExif
	}
	return parseTIFF(tiff)
}

// parseTIFF walks IFD0 for the EXIF and GPS sub-IFD pointers, then reads the four fields we care
// about. All offsets are relative to the TIFF header start.
func parseTIFF(t []byte) (Meta, error) {
	if len(t) < 8 {
		return Meta{}, errNoExif
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return Meta{}, errNoExif
	}
	if bo.Uint16(t[2:4]) != 42 {
		return Meta{}, errNoExif
	}
	ifd0 := int(bo.Uint32(t[4:8]))

	var m Meta
	var exifOff, gpsOff int
	walk(t, bo, ifd0, func(tag uint16, typ uint16, count uint32, val []byte) {
		switch tag {
		case 0x8769: // ExifIFDPointer
			if typ == 4 && count == 1 {
				exifOff = int(bo.Uint32(val))
			}
		case 0x8825: // GPSInfoIFDPointer
			if typ == 4 && count == 1 {
				gpsOff = int(bo.Uint32(val))
			}
		}
	})

	if exifOff > 0 {
		walk(t, bo, exifOff, func(tag uint16, typ uint16, count uint32, val []byte) {
			if tag == 0x9003 && typ == 2 { // DateTimeOriginal, ASCII "YYYY:MM:DD HH:MM:SS"
				s := asciiVal(val, count)
				if ts, err := time.Parse("2006:01:02 15:04:05", s); err == nil {
					m.TakenAt = ts
				}
			}
		})
	}

	if gpsOff > 0 {
		var latRef, lonRef string
		var lat, lon float64
		var haveLat, haveLon bool
		walk(t, bo, gpsOff, func(tag uint16, typ uint16, count uint32, val []byte) {
			switch tag {
			case 0x0001: // GPSLatitudeRef
				latRef = asciiVal(val, count)
			case 0x0002: // GPSLatitude, 3 rationals: deg, min, sec
				if typ == 5 && count == 3 {
					lat = dms(bo, val)
					haveLat = true
				}
			case 0x0003:
				lonRef = asciiVal(val, count)
			case 0x0004:
				if typ == 5 && count == 3 {
					lon = dms(bo, val)
					haveLon = true
				}
			}
		})
		if haveLat && haveLon {
			if latRef == "S" {
				lat = -lat
			}
			if lonRef == "W" {
				lon = -lon
			}
			m.Lat, m.Lon, m.HasGPS = lat, lon, true
		}
	}
	return m, nil
}

// walk iterates one IFD, handing each entry's tag, type, count, and its VALUE BYTES to fn. Per the
// TIFF spec, values whose encoded size fits in 4 bytes are inline; larger values live at an offset.
func walk(t []byte, bo binary.ByteOrder, off int, fn func(tag, typ uint16, count uint32, val []byte)) {
	if off < 0 || off+2 > len(t) {
		return
	}
	n := int(bo.Uint16(t[off : off+2]))
	pos := off + 2
	for e := 0; e < n; e++ {
		if pos+12 > len(t) {
			return
		}
		tag := bo.Uint16(t[pos : pos+2])
		typ := bo.Uint16(t[pos+2 : pos+4])
		count := bo.Uint32(t[pos+4 : pos+8])
		valField := t[pos+8 : pos+12]
		size := typeSize(typ) * int(count)
		var val []byte
		if size <= 4 && size > 0 {
			val = valField
		} else if size > 0 {
			vo := int(bo.Uint32(valField))
			if vo >= 0 && vo+size <= len(t) {
				val = t[vo : vo+size]
			}
		}
		if val != nil {
			fn(tag, typ, count, val)
		}
		pos += 12
	}
}

func typeSize(typ uint16) int {
	switch typ {
	case 1, 2, 6, 7: // byte, ascii, sbyte, undefined
		return 1
	case 3, 8: // short
		return 2
	case 4, 9, 11: // long, slong, float
		return 4
	case 5, 10, 12: // rational, srational, double
		return 8
	default:
		return 0
	}
}

// dms converts three EXIF rationals (degrees, minutes, seconds) to decimal degrees.
func dms(bo binary.ByteOrder, v []byte) float64 {
	if len(v) < 24 {
		return 0
	}
	r := func(o int) float64 {
		num := bo.Uint32(v[o : o+4])
		den := bo.Uint32(v[o+4 : o+8])
		if den == 0 {
			return 0
		}
		return float64(num) / float64(den)
	}
	return r(0) + r(8)/60 + r(16)/3600
}

// asciiVal trims the NUL terminator and any padding from an ASCII field.
func asciiVal(v []byte, count uint32) string {
	n := int(count)
	if n > len(v) {
		n = len(v)
	}
	s := v[:n]
	for len(s) > 0 && (s[len(s)-1] == 0 || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return string(s)
}
