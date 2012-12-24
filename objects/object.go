// Copyright 2012 Rémy Oudompheng. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// package objects deals with Git object format.
package objects

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"strconv"
)

type ObjType uint8

const (
	BLOB ObjType = iota
	TREE
	COMMIT
)

func matchType(t ObjType, s []byte) bool {
	switch t {
	case BLOB:
		return bytes.Equal(s, []byte("blob"))
	case TREE:
		return bytes.Equal(s, []byte("tree"))
	case COMMIT:
		return bytes.Equal(s, []byte("commit"))
	}
	return false
}

func (t ObjType) String() string {
	switch t {
	case BLOB:
		return "blob"
	case TREE:
		return "tree"
	case COMMIT:
		return "commit"
	}
	return fmt.Sprintf("BAD TYPE %d", int(t))
}

var (
	errCorruptedObjectHeader = errors.New("gigot: corrupted object header")
	errObjectSizeMismatch    = errors.New("gigot: object size mismatch")
)

type errInvalidType string

func (err errInvalidType) Error() string {
	return fmt.Sprintf("gigot: invalid object type %q", string(err))
}

// readLoose reads a Git object stored in loose format.
//
// A loose object consists of
// <type> <size>\x00
// where type is "blob", "tree" or "commit"
func readLoose(r io.ReadCloser) (t ObjType, s []byte, err error) {
	// read compressed data.
	zr, err := zlib.NewReader(r)
	if err != nil {
		r.Close()
		return
	}
	s, err = ioutil.ReadAll(zr)
	r.Close()

	hdr := s
	if len(hdr) > 32 {
		hdr = hdr[:32] // 32 bytes are enough for a 20-digit size.
	}
	if len(hdr) < 4 {
		err = errCorruptedObjectHeader
		return
	}
	sp := bytes.IndexByte(hdr, ' ')
	nul := bytes.IndexByte(hdr, 0)
	switch s[0] {
	case 'b':
		t = BLOB
	case 'c':
		t = COMMIT
	case 't':
		t = TREE
	}
	if sp < 0 || !matchType(t, hdr[:sp]) {
		err = errInvalidType(string(hdr[:sp]))
		return
	}
	if nul < 0 {
		err = errCorruptedObjectHeader
		return
	}
	sz, err := strconv.ParseUint(string(hdr[sp+1:nul]), 10, 64)
	if err != nil {
		err = errCorruptedObjectHeader
		return
	}
	s = s[nul+1:]
	if uint64(len(s)) != sz {
		err = errObjectSizeMismatch
	}
	return t, s, err
}
