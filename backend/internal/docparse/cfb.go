package docparse

import (
	"encoding/binary"
	"errors"
	"strings"
	"unicode/utf16"
)

// A reader for the OLE2 / Compound File container that legacy .xls files live in. It reads only what
// is needed to get at the "Workbook" stream, and it distrusts every offset and length it is given.

var oleMagic = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}

const (
	cfbEndOfChain = 0xFFFFFFFE
	cfbFreeSect   = 0xFFFFFFFF
	maxCFBStream  = 128 << 20
)

type cfbEntry struct {
	name  string
	typ   byte // 1 storage, 2 stream, 5 root
	start uint32
	size  uint64
}

type cfbFile struct {
	data       []byte
	sectorSize int
	miniSize   int
	cutoff     uint32
	fat        []uint32
	miniFAT    []uint32
	entries    []cfbEntry
	miniStream []byte
}

var errCFB = errors.New("not a valid compound file")

func le16(b []byte) int    { return int(binary.LittleEndian.Uint16(b)) }
func le32(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }

func openCFB(data []byte) (*cfbFile, error) {
	if len(data) < 512 || string(data[:8]) != string(oleMagic) {
		return nil, errCFB
	}
	c := &cfbFile{data: data}
	shift, miniShift := le16(data[30:]), le16(data[32:])
	if shift != 9 && shift != 12 || miniShift != 6 {
		return nil, errCFB
	}
	c.sectorSize, c.miniSize = 1<<shift, 1<<miniShift
	c.cutoff = le32(data[56:])
	numFAT, firstDir := int(le32(data[44:])), le32(data[48:])
	firstMini, numMini := le32(data[60:]), int(le32(data[64:]))
	firstDIFAT, numDIFAT := le32(data[68:]), int(le32(data[72:]))
	if numFAT < 0 || numFAT > len(data)/c.sectorSize+1 || numMini < 0 || numDIFAT < 0 || numDIFAT > len(data)/c.sectorSize+1 {
		return nil, errCFB
	}

	var fatSectors []uint32
	for i := 0; i < 109 && len(fatSectors) < numFAT; i++ {
		if s := le32(data[76+4*i:]); s != cfbFreeSect {
			fatSectors = append(fatSectors, s)
		}
	}
	next := firstDIFAT
	for i := 0; i < numDIFAT && next != cfbEndOfChain && next != cfbFreeSect; i++ {
		sec, ok := c.sector(next)
		if !ok {
			return nil, errCFB
		}
		per := c.sectorSize/4 - 1
		for j := 0; j < per && len(fatSectors) < numFAT; j++ {
			if s := le32(sec[4*j:]); s != cfbFreeSect {
				fatSectors = append(fatSectors, s)
			}
		}
		next = le32(sec[4*per:])
	}
	for _, s := range fatSectors {
		sec, ok := c.sector(s)
		if !ok {
			return nil, errCFB
		}
		for j := 0; j+4 <= len(sec); j += 4 {
			c.fat = append(c.fat, le32(sec[j:]))
		}
	}

	dirBytes, err := c.readChain(firstDir, -1)
	if err != nil {
		return nil, err
	}
	for off := 0; off+128 <= len(dirBytes); off += 128 {
		e := dirBytes[off : off+128]
		nameLen := le16(e[64:])
		if nameLen < 2 || nameLen > 64 {
			c.entries = append(c.entries, cfbEntry{})
			continue
		}
		u := make([]uint16, 0, nameLen/2)
		for j := 0; j+2 <= nameLen-2; j += 2 {
			u = append(u, uint16(le16(e[j:])))
		}
		size := binary.LittleEndian.Uint64(e[120:])
		if c.sectorSize == 512 {
			size &= 0xFFFFFFFF
		}
		c.entries = append(c.entries, cfbEntry{name: string(utf16.Decode(u)), typ: e[66], start: le32(e[116:]), size: size})
	}
	if len(c.entries) == 0 || c.entries[0].typ != 5 {
		return nil, errCFB
	}
	if numMini > 0 && firstMini != cfbEndOfChain {
		mb, err := c.readChain(firstMini, -1)
		if err != nil {
			return nil, err
		}
		for j := 0; j+4 <= len(mb); j += 4 {
			c.miniFAT = append(c.miniFAT, le32(mb[j:]))
		}
	}
	if root := c.entries[0]; root.size > 0 && root.size <= maxCFBStream {
		ms, err := c.readChain(root.start, int(root.size))
		if err != nil {
			return nil, err
		}
		c.miniStream = ms
	}
	return c, nil
}

func (c *cfbFile) sector(id uint32) ([]byte, bool) {
	off := (int64(id) + 1) * int64(c.sectorSize)
	if off < 0 || off+int64(c.sectorSize) > int64(len(c.data)) {
		return nil, false
	}
	return c.data[off : off+int64(c.sectorSize)], true
}

// readChain follows the FAT from start and returns up to limit bytes (all of them when limit < 0).
func (c *cfbFile) readChain(start uint32, limit int) ([]byte, error) {
	var out []byte
	id := start
	for steps := 0; id != cfbEndOfChain; steps++ {
		if steps > len(c.fat)+1 || int(id) >= len(c.fat) && len(c.fat) > 0 {
			return nil, errCFB
		}
		sec, ok := c.sector(id)
		if !ok {
			return nil, errCFB
		}
		out = append(out, sec...)
		if limit >= 0 && len(out) >= limit {
			return out[:limit], nil
		}
		if len(out) > maxCFBStream {
			return nil, errCFB
		}
		if len(c.fat) == 0 {
			break
		}
		id = c.fat[id]
	}
	if limit >= 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (c *cfbFile) readMiniChain(start uint32, size int) ([]byte, error) {
	var out []byte
	id := start
	for steps := 0; id != cfbEndOfChain && len(out) < size; steps++ {
		if steps > len(c.miniFAT)+1 || int(id) >= len(c.miniFAT) {
			return nil, errCFB
		}
		off := int(id) * c.miniSize
		if off < 0 || off+c.miniSize > len(c.miniStream) {
			return nil, errCFB
		}
		out = append(out, c.miniStream[off:off+c.miniSize]...)
		id = c.miniFAT[id]
	}
	if len(out) < size {
		return nil, errCFB
	}
	return out[:size], nil
}

// stream returns the bytes of the named stream (case-insensitive), or nil when there is none.
func (c *cfbFile) stream(names ...string) ([]byte, error) {
	for _, e := range c.entries {
		if e.typ != 2 {
			continue
		}
		for _, n := range names {
			if strings.EqualFold(e.name, n) {
				if e.size > maxCFBStream {
					return nil, errCFB
				}
				if e.size < uint64(c.cutoff) {
					return c.readMiniChain(e.start, int(e.size))
				}
				return c.readChain(e.start, int(e.size))
			}
		}
	}
	return nil, nil
}
