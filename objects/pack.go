// Copyright 2012 Rémy Oudompheng. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package objects

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
)

// This file implements Git's packfile format.
//
// Cf. Documentation/technical/pack-format.txt in Git sources for
// reference.

type PackReader struct {
	version   int
	pack, idx File

	// idxFanout[i] is the number of objects whose first byte
	// is <= i.
	idxFanout [256]uint32
}

type File interface {
	io.ReaderAt
	io.Closer
}

var (
	errBadPackMagic           = errors.New("gigot: bad magic number in packfile")
	errBadIdxMagic            = errors.New("gigot: bad magic number in index file")
	errUnsupportedPackVersion = errors.New("gigot: packfile has unsupported format version")
)

func NewPackReader(pack, idx File) (*PackReader, error) {
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

func checkPackMagic(pack File) (version, count uint32, err error) {
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

func (pk *PackReader) checkIdxMagic(idx File) (err error) {
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

func (pk *PackReader) findObject(hash [20]byte) (offset int64, err error) {
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
	for max-min >= 2 {
		var hmed [20]byte
		med := (min + max) / 2
		_, err = pk.idx.ReadAt(hmed[:], idxHeaderSize+med*20)
		if err != nil {
			return 0, err
		}
		switch cmp := bytes.Compare(hmed[:], hash[:]); true {
		case cmp < 0:
			min = med
		case cmp > 0:
			max = med
		case cmp == 0:
			// Found.
			min, max = med, med
			break BinarySearch
		}
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

func (pk *PackReader) Close() error {
	err1 := pk.pack.Close()
	err2 := pk.idx.Close()
	if err1 != nil {
		return err1
	}
	return err2
}
