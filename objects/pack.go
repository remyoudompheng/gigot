// Copyright 2012 Rémy Oudompheng. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package objects

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"io"

	"github.com/remyoudompheng/gigot/gitdelta"
)

// This file implements Git's packfile format.
//
// Cf. Documentation/technical/pack-format.txt in Git sources for
// reference.

// A PackReader implements access to Git pack files and indexes.
type PackReader struct {
	version   int
	pack, idx *io.SectionReader

	// idxFanout[i] is the number of objects whose first byte
	// is <= i.
	idxFanout [256]uint32
}

var (
	errBadPackMagic           = errors.New("gigot: bad magic number in packfile")
	errBadIdxMagic            = errors.New("gigot: bad magic number in index file")
	errUnsupportedPackVersion = errors.New("gigot: packfile has unsupported format version")
	errInvalidPackEntryType   = errors.New("gigot: invalid type for packfile entry")
)

// NewPackReader creates a PackReader from files pointing to a packfile
// and its index.
func NewPackReader(pack, idx *io.SectionReader) (*PackReader, error) {
	version, _, err := checkPackMagic(pack)
	if err != nil {
		return nil, err
	}
	pk := &PackReader{version: int(version), pack: pack, idx: idx}
	err = pk.checkIdxMagic(idx)
	if err != nil {
		return nil, err
	}
	return pk, err
}

func checkPackMagic(pack *io.SectionReader) (version, count uint32, err error) {
	var buf [12]byte
	_, err = pack.ReadAt(buf[:], 0)
	if err != nil {
		return
	}
	magic := [4]byte{buf[0], buf[1], buf[2], buf[3]}
	if magic != ([4]byte{'P', 'A', 'C', 'K'}) {
		err = errBadPackMagic
	}
	version = binary.BigEndian.Uint32(buf[4:8])
	if version != 2 {
		err = errUnsupportedPackVersion
	}
	count = binary.BigEndian.Uint32(buf[4:8])
	return
}

const idxHeaderSize = 4 + 4 + 256*4

func (pk *PackReader) checkIdxMagic(idx *io.SectionReader) (err error) {
	var buf [idxHeaderSize]byte
	_, err = idx.ReadAt(buf[:], 0)
	if err != nil {
		return
	}
	magic := [4]byte{buf[0], buf[1], buf[2], buf[3]}
	if magic != ([4]byte{'\xff', 't', 'O', 'c'}) {
		return errBadIdxMagic
	}
	for i := range pk.idxFanout {
		pk.idxFanout[i] = binary.BigEndian.Uint32(buf[8+4*i:])
	}
	return nil
}

var errNotFoundInPack = errors.New("object does not exist in pack")

func (pk *PackReader) findObject(hash Hash) (offset int64, err error) {
	min, max := int64(0), int64(pk.idxFanout[hash[0]])
	if hash[0] > 0 {
		min = int64(pk.idxFanout[hash[0]-1])
	}
	var hmin, hmax [20]byte
	_, err = pk.idx.ReadAt(hmin[:], idxHeaderSize+min*20)
	if err != nil {
		return 0, err
	}
	_, err = pk.idx.ReadAt(hmax[:], idxHeaderSize+max*20)
	if err != nil {
		return 0, err
	}
BinarySearch:
	for min < max {
		var hmed [20]byte
		med := (min + max) / 2
		_, err = pk.idx.ReadAt(hmed[:], idxHeaderSize+med*20)
		if err != nil {
			return 0, err
		}
		switch cmp := bytes.Compare(hmed[:], hash[:]); true {
		case cmp < 0:
			min = med + 1
		case cmp > 0:
			max = med - 1
		case cmp == 0:
			// Found.
			min, max = med, med
			break BinarySearch
		}
	}
	if min > max {
		return 0, errNotFoundInPack
	}

	// Read from 32-bit offset table.
	// The index contains objcount 20-byte hashes, and objcount
	// 32-bit CRC32 sums.
	objcount := int64(pk.idxFanout[0xff])
	var offb [8]byte
	_, err = pk.idx.ReadAt(offb[:4], idxHeaderSize+24*objcount+4*min)
	if err != nil {
		return 0, err
	}
	off32 := int32(binary.BigEndian.Uint32(offb[:4]))
	if off32 >= 0 {
		return int64(off32), nil
	}

	// Read from 64-bit offset table.
	_, err = pk.idx.ReadAt(offb[:8], idxHeaderSize+28*objcount+8*min)
	off64 := int64(binary.BigEndian.Uint64(offb[:]))
	return off64, err
}

const (
	pkNone = iota
	pkCommit
	pkTree
	pkBlob
	pkTag
	_
	pkOfsDelta
	pkRefDelta
	pkAny
	pkMax
	pkBad = -1
)

// Extract finds and parses an object from a pack.
func (pk *PackReader) Extract(h Hash) (Object, error) {
	typ, data, err := pk.extract(h)
	if err != nil {
		return nil, err
	}
	switch typ {
	case pkCommit:
		return readObject(COMMIT, data)
	case pkTree:
		return readObject(TREE, data)
	case pkBlob:
		return readObject(BLOB, data)
	}
	println(typ)
	panic("invalid pack object type")
}

// extract extracts the raw contents of an object.
func (pk *PackReader) extract(h Hash) (typ int, data []byte, err error) {
	// An object entry in a packfile has the following form:
	// <VARINT><L bytes>
	// The varint encodes the object type and length in the following way:
	//   size = high<<4 | low
	//   VARINT = high<<7 | type<<4 | low.
	//
	// Object types in pack are described by enum object_type in
	// Git sources (cache.h)
	off, err := pk.findObject(h)
	if err != nil {
		return
	}
	return pk.extractAt(off)
}

func (pk *PackReader) extractAt(off int64) (typ int, data []byte, err error) {
	var buf [16]byte // 109-bit sizes should be enough for everybody.
	_, err = pk.pack.ReadAt(buf[:], off)
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		err = nil
	}
	if err != nil {
		return
	}
	varint, n := binary.Uvarint(buf[:])
	objsize := int64((varint>>7)<<4 | (varint & 0xf))
	objtype := int(varint>>4) & 0x7 // 3 bits.

	switch objtype {
	case pkCommit, pkTree, pkBlob, pkTag:
		// objsize is the *uncompressed* size.
		data = make([]byte, objsize)
		n, err := readCompressed(pk.pack, off+int64(n), data)
		return objtype, data[:n], err
	case pkRefDelta:
		// Ref delta: parent hash (20 bytes) + deflated delta (objsize bytes)
		var parent Hash
		_, err := pk.pack.ReadAt(parent[:], off+int64(n))
		if err != nil {
			return typ, data, err
		}
		patch := make([]byte, objsize)
		_, err = readCompressed(pk.pack, off+int64(n)+20, patch)
		// FIXME: check that parent object is always in the same pack.
		typ, data, err = pk.extract(parent)
		if err != nil {
			return typ, patch, err
		}
		data, err = gitdelta.Patch(data, patch)
		if err != nil {
			return typ, patch, err
		}
		return typ, data, err
	case pkOfsDelta:
		// Offset delta: distance to parent (varint bytes) + deflated delta (objsize bytes)
		parentOff, n2, err := readVaroffset(pk.pack, off+int64(n))
		if err != nil {
			return objtype, data, err
		}
		patch := make([]byte, objsize)
		_, err = readCompressed(pk.pack, off+int64(n+n2), patch)
		typ, data, err = pk.extractAt(off - parentOff)
		if err != nil {
			return typ, patch, err
		}
		data, err = gitdelta.Patch(data, patch)
		if err != nil {
			return typ, patch, err
		}
		return typ, data, err
	}
	return typ, data, errInvalidPackEntryType
}

// Objects returns the list of hashes of objects stored in this pack.
func (pk *PackReader) Objects() ([]Hash, error) {
	count := pk.idxFanout[0xff]
	buf := make([]byte, 20*count)
	_, err := pk.idx.ReadAt(buf, idxHeaderSize)
	if err != nil {
		return nil, err
	}
	hashes := make([]Hash, count)
	for i := range hashes {
		copy(hashes[i][:], buf[20*i:20*(i+1)])
	}
	return hashes, nil
}

// Utility functions.

func readVarint(r *io.SectionReader, offset int64) (v int64, n int, err error) {
	var buf [16]byte // 109 bits should be enough for everybody.
	_, err = r.ReadAt(buf[:], offset)
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		err = nil
	}
	if err != nil {
		return
	}
	u, n := binary.Uvarint(buf[:])
	v = int64(u)
	return
}

// readVaroffset reads the pseudo-varint used to encode offsets for delta bases.
// It is a big-endian form: 1|a0, ..., 1|a_{n-1}, 0|a_n.
// representing:
//  (a0+1)<<7*n + ... + (a_{n-1}+1)<<7 + a_n
func readVaroffset(r *io.SectionReader, offset int64) (v int64, n int, err error) {
	var buf [16]byte // 109 bits should be enough for everybody.
	n, err = r.ReadAt(buf[:], offset)
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		err = nil
	}
	if err != nil {
		return
	}
	u := uint64(0)
	for i, b := range buf[:n] {
		if i > 0 {
			u++
		}
		u <<= 7
		u |= uint64(b &^ 0x80)
		if b&0x80 == 0 {
			return int64(u), i + 1, nil
		}
	}
	return int64(u), len(buf), io.ErrUnexpectedEOF
}

func readCompressed(r *io.SectionReader, offset int64, s []byte) (int, error) {
	zr, err := zlib.NewReader(io.NewSectionReader(r, offset, r.Size()-offset))
	if err != nil {
		return 0, err
	}
	return io.ReadFull(zr, s)
}
