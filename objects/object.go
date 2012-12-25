// Copyright 2012 Rémy Oudompheng. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// package objects deals with Git object format.
package objects

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
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

type Object interface {
	ID() Hash
	Type() ObjType
	WriteTo(io.Writer) error
}

type Hash [20]byte

func (h Hash) String() string {
	return hex.EncodeToString(h[:])
}

func NewHash(s []byte) (b Hash) {
	h := sha1.New()
	h.Write(s)
	h.Sum(b[:0])
	return
}

// A Blob is an object representing a chunk of data.
type Blob struct {
	Hash Hash
	Data []byte
}

func (b Blob) ID() Hash      { return b.Hash }
func (b Blob) Type() ObjType { return BLOB }

func (b Blob) WriteTo(w io.Writer) error {
	_, err := fmt.Fprintf(w, "blob %d\x00", len(b.Data))
	if err == nil {
		_, err = w.Write(b.Data)
	}
	return err
}

// A Tree represents a collection of blobs and trees
// in a directory-like fashion.
type Tree struct {
	Hash    Hash
	Entries []TreeElem
}

type TreeElem struct {
	Name string
	Mode os.FileMode
	Hash Hash
}

func (t Tree) ID() Hash      { return t.Hash }
func (t Tree) Type() ObjType { return TREE }

func (t Tree) WriteTo(w io.Writer) error {
	// The format of a tree object is:
	// (OctalMode " " Name "\x00" Hash)*
	length := 0
	for _, e := range t.Entries {
		length += (6 + 1 + 1 + 20) + len(e.Name)
	}
	_, err := fmt.Fprintf(w, "tree %d\x00", length)
	if err != nil {
		return err
	}
	for _, entry := range t.Entries {
		_, err = fmt.Fprintf(w, "%06o %s\x00%s", gitMode(entry.Mode), entry.Name, entry.Hash[:])
		if err != nil {
			return err
		}
	}
	return nil
}

// A Git mode is (type<<12|unixperm).
type Mode uint16

const (
	ModeRegular Mode = 8 << 12
	ModeDir     Mode = 4 << 12
	ModeSymlink Mode = 2 << 12

	ModeGitlink = ModeDir | ModeSymlink
)

// gitMode computes the file mode bits as expected by Git.
func gitMode(mode os.FileMode) Mode {
	m := Mode(mode & os.ModePerm)
	if mode&os.ModeDir != 0 {
		m |= ModeDir
	}
	if mode&os.ModeSymlink != 0 {
		m |= ModeSymlink
	}
	if mode&os.ModeType == 0 {
		m |= ModeRegular
	}
	return m
}

func osMode(mode Mode) os.FileMode {
	m := os.FileMode(mode & 0777)
	if mode&ModeRegular != 0 {
		return m
	}
	if mode&ModeDir != 0 {
		m |= os.ModeDir
	}
	if mode&ModeSymlink != 0 {
		m |= os.ModeSymlink
	}
	return m
}

var (
	errBadTreeData = errors.New("gigot: bad tree data format")
)

func parseTree(s []byte) (t Tree, err error) {
	for len(s) > 0 {
		sp := bytes.IndexByte(s, ' ')
		nul := bytes.IndexByte(s, '\x00')
		switch {
		case sp != 6, nul < sp, nul+20 >= len(s):
			println(sp, nul, len(s))
			err = errBadTreeData
			return
		}
		var e TreeElem
		mode, err := strconv.ParseUint(string(s[:sp]), 8, 16)
		if err != nil {
			return t, err
		}
		e.Mode = osMode(Mode(mode))
		e.Name = string(s[sp+1 : nul])
		copy(e.Hash[:], s[nul+1:nul+21])
		s = s[nul+21:]
		t.Entries = append(t.Entries, e)
	}
	return t, nil
}
