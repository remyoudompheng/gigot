// Copyright 2012 Rémy Oudompheng. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package objects

import (
	"bytes"
	"encoding/hex"
	"os"
	"testing"
)

// The three test objects were created by committing a single file
// "test", with content "Hello World!\n", in an empty Git repository.
//
// $ git init
// $ echo "Hello World!" > test
// $ git add test
// $ git commit -m "Hello!"

func TestReadLooseBlob(t *testing.T) {
	f, err := os.Open("testdata/loose-blob-980a0d5f19a64b4b30a87d4206aade58726b60e3")
	if err != nil {
		t.Fatal(err)
	}
	t1, s1, err := readLoose(f)
	switch {
	case err != nil:
		t.Error(err)
	case t1 != BLOB:
		t.Errorf("bad type %v, expected %v", t1, BLOB)
	case string(s1) != "Hello World!\n":
		t.Errorf("bad content %q, expected %q",
			s1, "Hello World!\n")
	}
}

func binaryHash(hexhash string) string {
	var hash [20]byte
	n, err := hex.Decode(hash[:], []byte(hexhash))
	if n != 20 {
		panic("n != 20")
	}
	if err != nil {
		panic(err)
	}
	return string(hash[:])
}

func TestReadLooseTree(t *testing.T) {
	f, err := os.Open("testdata/loose-tree-504094bacb51b85f453161900acc5989f2f38688")
	if err != nil {
		t.Fatal(err)
	}
	t1, s1, err := readLoose(f)
	expect := "100644 test\x00" + binaryHash("980a0d5f19a64b4b30a87d4206aade58726b60e3")
	switch {
	case err != nil:
		t.Error(err)
	case t1 != TREE:
		t.Errorf("bad type %v, expected %v", t1, TREE)
	case string(s1) != expect:
		t.Errorf("bad content %q, expected %q", s1, expect)
	}
}

func TestReadLooseCommit(t *testing.T) {
	f, err := os.Open("testdata/loose-commit-cff5570614ef7eb3620e0e98f9938e8ade423e1a")
	if err != nil {
		t.Fatal(err)
	}
	t1, s1, err := readLoose(f)
	const expect = `tree 504094bacb51b85f453161900acc5989f2f38688
author Rémy Oudompheng <remy@archlinux.org> 1356355981 +0100
committer Rémy Oudompheng <remy@archlinux.org> 1356355981 +0100

Hello!
`
	switch {
	case err != nil:
		t.Error(err)
	case t1 != COMMIT:
		t.Errorf("bad type %v, expected %v", t1, COMMIT)
	case string(s1) != expect:
		t.Errorf("bad content %q, expected %q", s1, expect)
	}
}

func TestWriteTree(t *testing.T) {
	expect := "tree 58\x00" +
		"100644 a\x00" + binaryHash("e965047ad7c57865823c7d992b1d046ea66edf78") +
		"100644 b\x00" + binaryHash("216e97ce08229b8776d3feb731c6d23a2f669ac8")
	tree, err := parseTree([]byte(expect[8:]))
	if err != nil {
		t.Fatal("parse tree:", err)
	}
	buf := new(bytes.Buffer)
	tree.WriteTo(buf)
	if buf.String() != expect {
		t.Errorf("got %q, expected %q", buf, expect)
	}

	h := NewHash([]byte(expect))
	if h.String() != "8860cd0334e8b582ec8fe85a99dcc58ad6ee9387" {
		t.Errorf("got hash %s, expected %s", h,
			"8860cd0334e8b582ec8fe85a99dcc58ad6ee9387")
	}
}
