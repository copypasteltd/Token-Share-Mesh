package comfyenv

import (
	"context"
	"encoding/binary"
	"os"
)

// Inspect the bounded ZIP central-directory metadata BEFORE archive/zip can
// allocate one entry per record. Large complete environments must be split
// into exact artifacts; ZIP64 is accepted only as a single-disk fixed record.
const maximumArchiveEntries = 100000
const maximumDirectoryBytes = 32 << 20

func archiveBounds(ctx context.Context, f *os.File, size int64) error {
	if ctx == nil || ctx.Err() != nil {
		return ErrFiles
	}
	if size < 22 {
		return ErrFiles
	}
	n := int64(22 + 65535)
	if size < n {
		n = size
	}
	tail := make([]byte, int(n))
	if _, err := f.ReadAt(tail, size-n); err != nil {
		return ErrFiles
	}
	for at := len(tail) - 22; at >= 0; at-- {
		if ctx.Err() != nil {
			return ErrFiles
		}
		b := tail[at:]
		if binary.LittleEndian.Uint32(b) != 0x06054b50 || int(binary.LittleEndian.Uint16(b[20:]))+22 != len(b) {
			continue
		}
		end := size - n + int64(at)
		if binary.LittleEndian.Uint16(b[4:]) != 0 || binary.LittleEndian.Uint16(b[6:]) != 0 {
			return ErrFiles
		}
		count := uint64(binary.LittleEndian.Uint16(b[10:]))
		if binary.LittleEndian.Uint16(b[8:]) != uint16(count) {
			return ErrFiles
		}
		directorySize := uint64(binary.LittleEndian.Uint32(b[12:]))
		offset := uint64(binary.LittleEndian.Uint32(b[16:]))
		var locator [20]byte
		zip64 := false
		if end >= 20 {
			if _, err := f.ReadAt(locator[:], end-20); err != nil {
				return ErrFiles
			}
			zip64 = binary.LittleEndian.Uint32(locator[:]) == 0x07064b50
		}
		if count == 65535 || directorySize == 0xffffffff || offset == 0xffffffff || zip64 {
			if !zip64 || binary.LittleEndian.Uint32(locator[4:]) != 0 || binary.LittleEndian.Uint32(locator[16:]) != 1 {
				return ErrFiles
			}
			position := binary.LittleEndian.Uint64(locator[8:])
			if position > uint64(end-20) || uint64(end-20)-position != 56 {
				return ErrFiles
			}
			var record [56]byte
			if _, err := f.ReadAt(record[:], int64(position)); err != nil || binary.LittleEndian.Uint32(record[:]) != 0x06064b50 || binary.LittleEndian.Uint64(record[4:]) != 44 || binary.LittleEndian.Uint32(record[16:]) != 0 || binary.LittleEndian.Uint32(record[20:]) != 0 {
				return ErrFiles
			}
			count = binary.LittleEndian.Uint64(record[32:])
			if binary.LittleEndian.Uint64(record[24:]) != count {
				return ErrFiles
			}
			directorySize = binary.LittleEndian.Uint64(record[40:])
			offset = binary.LittleEndian.Uint64(record[48:])
			end = int64(position)
		}
		if count == 0 || count > maximumArchiveEntries || directorySize < count*46 || directorySize > maximumDirectoryBytes || offset > uint64(end) || directorySize > uint64(end)-offset {
			return ErrFiles
		}
		// Count the actual records with one fixed buffer. A forged small EOCD
		// count must not make archive/zip allocate a much larger directory.
		cursor := offset
		limit := offset + directorySize
		var header [46]byte
		for seen := uint64(0); seen < count; seen++ {
			if ctx.Err() != nil {
				return ErrFiles
			}
			if limit-cursor < uint64(len(header)) {
				return ErrFiles
			}
			if _, err := f.ReadAt(header[:], int64(cursor)); err != nil || binary.LittleEndian.Uint32(header[:]) != 0x02014b50 {
				return ErrFiles
			}
			length := uint64(len(header)) + uint64(binary.LittleEndian.Uint16(header[28:])) + uint64(binary.LittleEndian.Uint16(header[30:])) + uint64(binary.LittleEndian.Uint16(header[32:]))
			if length > limit-cursor {
				return ErrFiles
			}
			cursor += length
		}
		if cursor != limit {
			return ErrFiles
		}
		return nil
	}
	return ErrFiles
}
