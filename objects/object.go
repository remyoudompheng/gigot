// Copyright 2012 Rémy Oudompheng. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// package objects deals with Git object format.
//
// It implements low-level accessors to read and parse
// loose objects and packfiles in Git repositories, and
// defines appropriate data types representing the three
// basic object types of Git: blobs, trees and commits.
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
	"time"
)

// ObjType enumerates the three possible object types: blob, tree, commit.
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

func readObject(t ObjType, data []byte) (Object, error) {
	switch t {
	case BLOB:
		o := Blob{Data: data}
		o.Hash = rehash(o)
		return o, nil
	case TREE:
		o, err := parseTree(data)
		o.Hash = rehash(o)
		return o, err
	case COMMIT:
		o, err := parseCommit(data)
		o.Hash = rehash(o)
		return o, err
	}
	panic(errInvalidType(t.String()))
}

// ParseLoose reads a loose object as stored in the objects/
// subdirectory of a git repository.
func ParseLoose(r io.ReadCloser) (Object, error) {
	t, data, err := readLoose(r)
	if err != nil {
		return nil, err
	}
	return readObject(t, data)
}

type Object interface {
	// ID return the hash of the object.
	ID() Hash
	// Type returns the object type (BLOB, TREE, COMMIT).
	Type() ObjType
	// WriteTo serializes the object: if the Writer is a sha1 Hash
	// this will produce the hash for this object, if the Writer is
	// a compressor from compress/flate, this is equivalent to the
	// loose object format.
	WriteTo(io.Writer) error
}

func rehash(o Object) (out Hash) {
	h := sha1.New()
	o.WriteTo(h)
	h.Sum(out[:0])
	return
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
	buf := new(bytes.Buffer)
	for _, entry := range t.Entries {
		fmt.Fprintf(buf, "%o %s\x00%s", gitMode(entry.Mode), entry.Name, entry.Hash[:])
	}
	_, err := fmt.Fprintf(w, "tree %d\x00", buf.Len())
	if err != nil {
		return err
	}
	_, err = w.Write(buf.Bytes())
	return err
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
		case nul < sp, nul+20 >= len(s):
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

// A Commit represents the metadata stored in a Git commit.
type Commit struct {
	Hash          Hash
	Tree          Hash   // The tree object pointed to by this commit.
	Parents       []Hash // The parents of this commit.
	Author        string // The email address of the commit author.
	AuthorTime    time.Time
	Committer     string // The email address of the committer.
	CommitterTime time.Time
	Message       []byte // The commit description.
}

func (c Commit) ID() Hash      { return c.Hash }
func (c Commit) Type() ObjType { return COMMIT }

func (c Commit) WriteTo(w io.Writer) error {
	buf := new(bytes.Buffer)
	fmt.Fprintf(buf, "tree %s\n", c.Tree)
	for _, p := range c.Parents {
		fmt.Fprintf(buf, "parent %s\n", p)
	}
	fmt.Fprintf(buf, "author %s %d %s\n", c.Author, c.AuthorTime.Unix(), c.AuthorTime.Format("-0700"))
	fmt.Fprintf(buf, "committer %s %d %s\n", c.Committer, c.CommitterTime.Unix(), c.CommitterTime.Format("-0700"))
	fmt.Fprintf(buf, "\n%s", c.Message)

	fmt.Fprintf(w, "commit %d\x00", buf.Len())
	_, err := w.Write(buf.Bytes())
	return err
}

func parseCommit(s []byte) (c Commit, err error) {
	// Reference: git/Documentation/user-manual.txt, Commit object.

	// Header lines until an empty line.
	for len(s) > 0 {
		i := bytes.IndexByte(s, '\n')
		if i < 0 {
			return c, errMalformedCommitLine
		}
		line := s[:i]
		s = s[i+1:]
		if len(line) == 0 {
			break
		}
		// read the first word.
		sp := bytes.IndexByte(line, ' ')
		if sp < 0 {
			return c, errMalformedCommitLine
		}
		switch word := string(line[:sp]); word {
		case "tree":
			n, err := hex.Decode(c.Tree[:], line[sp+1:])
			if err != nil || n != len(c.Hash) {
				return c, errMalformedCommitLine
			}
		case "parent":
			var parent Hash
			n, err := hex.Decode(parent[:], line[sp+1:])
			if err != nil || n != len(parent) {
				return c, errMalformedCommitLine
			}
			c.Parents = append(c.Parents, parent)
		case "author":
			c.Author, c.AuthorTime, err = parseAuthor(line[sp+1:])
		case "committer":
			c.Committer, c.CommitterTime, err = parseAuthor(line[sp+1:])
		default:
			panic("unrecognized commit header " + word)
		}
	}
	c.Message = append(c.Message, s...)
	return c, nil
}

var errMalformedCommitLine = errors.New("gigot: malformed commit line")

// parseAuthor parses a commit author description.
func parseAuthor(line []byte) (name string, when time.Time, err error) {
	// John Doe <john.doe@example.com> UNIXTIME ±0700
	sp1 := bytes.LastIndexAny(line, " ")
	if sp1 < 0 {
		err = errMalformedCommitLine
		return
	}
	sp2 := bytes.LastIndexAny(line[:sp1], " ")
	if sp2 < 0 {
		err = errMalformedCommitLine
		return
	}
	if sp1+6 != len(line) {
		err = errMalformedCommitLine
		return
	}
	z := string(line[sp2:])
	unix, err := strconv.ParseInt(z[1:sp1-sp2], 10, 64)
	if err != nil {
		return
	}
	z = z[sp1-sp2:]
	zhour, err := strconv.Atoi(z[1:4])
	if err != nil {
		return
	}
	zmin, err := strconv.Atoi(z[4:6])
	if err != nil {
		return
	}
	return string(line[:sp2]), time.Unix(unix, 0).In(time.FixedZone(z[1:], zhour*3600+zmin*60)), nil
}
