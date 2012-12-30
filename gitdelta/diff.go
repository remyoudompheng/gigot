// Copyright 2012 Rémy Oudompheng. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gitdelta

import (
	"bytes"
	"encoding/binary"
)

// This file uses Rabin hashing to delta encode.

// hashChunks hashes chunks input[k*W+1 : (k+1)*W]
// and returns a map from hashes to index in input buffer.
func hashChunks(input []byte) map[uint32]int {
	nbHash := len(input) / _W
	hashes := make(map[uint32]int, nbHash)
	for i := (nbHash - 1) * _W; i > 0; i -= _W {
		// on collision overwrite with smallest index.
		h := hashRabin(input[i : i+_W])
		hashes[h] = i
	}
	return hashes
}

// Diff computes a delta from data1 to data2. The
// result is such that Patch(data1, Diff(data1, data2)) == data2.
func Diff(data1, data2 []byte) []byte {
	// Store lengths of inputs.
	patch := make([]byte, 32)
	n1 := binary.PutUvarint(patch, uint64(len(data1)))
	n2 := binary.PutUvarint(patch[n1:], uint64(len(data2)))
	patch = patch[:n1+n2]

	// First hash chunks of data1.
	hashes := hashChunks(data1)

	// Compute rolling hashes of data2 and see whether
	// we recognize parts of data1.
	var p uint32
	lastmatch := -1
	for i := 0; i < len(data2); i++ {
		b := data2[i]
		if i < _W {
			p = (p << 8) ^ uint32(b) ^ _T[uint8(p>>(degree-8))]
			continue
		}
		// Invariant: i >= W and p == hashRabin(data2[i-W:i])
		//if p != hashRabin(data2[i-_W:i]) {
		//	println(p, hashRabin(data2[i-_W:i]))
		//	panic("p != hashRabin(data2[i-_W:i])")
		//}

		refi, ok := hashes[p]
		if ok && bytes.Equal(data1[refi:refi+_W], data2[i-_W:i]) {
			// We have a match! Try to extend it left and right.
			testi := i - _W
			for refi > 0 && testi > lastmatch+1 && data1[refi-1] == data2[testi-1] {
				refi--
				testi--
			}
			refj, testj := refi+i-testi, i
			for refj < len(data1) && testj < len(data2) && data1[refj] == data2[testj] {
				refj++
				testj++
			}

			// Now data1[refi:refj] == data2[testi:testj]
			patch = appendInlineData(patch, data2[lastmatch+1:testi])
			patch = appendRefData(patch, uint32(refi), uint32(refj-refi))

			// Skip bytes and update hash.
			skipped := data2[i:]
			if testj+_W < len(data2) {
				skipped = data2[i : testj+_W]
			}
			for tmp, b := range skipped {
				p ^= _U[data2[(i+tmp)-_W]]
				p = (p << 8) ^ uint32(b) ^ _T[uint8(p>>(degree-8))]
			}
			lastmatch = testj - 1
			if i+len(skipped) == len(data2) {
				break
			}
			i += len(skipped)
			b = data2[i]
		}

		// Cancel out data2[i-W] and take data2[i]
		p ^= _U[data2[i-_W]]
		p = (p << 8) ^ uint32(b) ^ _T[uint8(p>>(degree-8))]
	}
	patch = appendInlineData(patch, data2[lastmatch+1:])
	return patch
}

// appendInlineData encodes inline data in a patch.
func appendInlineData(patch, data []byte) []byte {
	for len(data) > 0x7f {
		patch = append(patch, 0x7f)
		patch = append(patch, data[:0x7f]...)
		data = data[0x7f:]
	}
	if len(data) > 0 {
		patch = append(patch, byte(len(data)))
		patch = append(patch, data...)
	}
	return patch
}

// appendRefData encodes reference to original data in a delta.
func appendRefData(patch []byte, off, length uint32) []byte {
	for length > 1<<16 {
		// emit opcode for length 1<<16.
		switch {
		case off>>8 == 0:
			patch = append(patch, 0x81, byte(off))
		case off>>16 == 0:
			patch = append(patch, 0x83, byte(off), byte(off>>8))
		case off>>24 == 0:
			patch = append(patch, 0x87,
				byte(off), byte(off>>8), byte(off>>16))
		default:
			patch = append(patch, 0x8f,
				byte(off), byte(off>>8), byte(off>>16), byte(off>>24))
		}
		off += 1 << 16
		length -= 1 << 16
	}

	iop := len(patch)
	patch = append(patch, 0)
	op := byte(0x80)

	if b := byte(off); b != 0 {
		op |= 1
		patch = append(patch, b)
	}
	if b := byte(off >> 8); b != 0 {
		op |= 2
		patch = append(patch, b)
	}
	if b := byte(off >> 16); b != 0 {
		op |= 2
		patch = append(patch, b)
	}
	if b := byte(off >> 24); b != 0 {
		op |= 2
		patch = append(patch, b)
	}

	if b := byte(length); b != 0 {
		op |= 0x10
		patch = append(patch, b)
	}
	if b := byte(length >> 8); b != 0 {
		op |= 0x20
		patch = append(patch, b)
	}

	patch[iop] = op
	return patch
}
