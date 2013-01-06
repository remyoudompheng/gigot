// Copyright 2012 Rémy Oudompheng. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package objects

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func getPack(t *testing.T) *PackReader {
	// Take a random packfile in our own repository.
	packs, err := filepath.Glob("../.git/objects/pack/pack-*.pack")
	if err != nil || len(packs) == 0 {
		t.Fatalf("globbing failed: %v", err, packs)
	}
	pname := packs[0]
	pack, err := os.Open(pname)
	if err != nil {
		t.Fatalf("count not open %s: %s", pname, err)
	}
	idx, err := os.Open(pname[:len(pname)-5] + ".idx")
	if err != nil {
		t.Fatal(err)
	}
	packstat, err := pack.Stat()
	if err != nil {
		t.Fatal("stat pack", err)
	}
	idxstat, err := idx.Stat()
	if err != nil {
		t.Fatal("stat idx", err)
	}
	t.Logf("opening pack %s (%d bytes)", pname, packstat.Size())
	pk, err := NewPackReader(
		io.NewSectionReader(pack, 0, packstat.Size()),
		io.NewSectionReader(idx, 0, idxstat.Size()))
	if err != nil {
		t.Fatal(err)
	}
	return pk
}

func TestFindInPack(t *testing.T) {
	pk := getPack(t)
	// Take the object ID of a random ref.
	refs, err := ioutil.ReadFile("../.git/info/refs")
	if err != nil {
		refs, err = ioutil.ReadFile("../.git/refs/heads/master")
	}
	if err != nil {
		t.Fatal(err)
	}
	id := bytes.Fields(refs)[0]
	if len(id) != 40 {
		t.Fatal("invalid commit ID %q in info/refs", id)
	}
	var refhash [20]byte
	hex.Decode(refhash[:], id)
	t.Logf("lookup %040x", refhash)
	off, err := pk.findObject(refhash)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("offset=%v", off)
}

func TestDumpPack(t *testing.T) {
	pk := getPack(t)
	hashes, err := pk.Objects()
	if err != nil {
		t.Fatal("list pack", err)
	}
	for i, h := range hashes {
		typ, data, err := pk.extract(h)
		if len(data) < 80 {
			t.Logf("%s %d %+q", h, typ, data)
		} else {
			t.Logf("%s %d (%d bytes)", h, typ, len(data))
		}
		if err != nil {
			t.Fatal("extract", h, err)
			break
		}
		if i > 20 {
			break
		}
	}
}

func TestDisplayPack(t *testing.T) {
	pk := getPack(t)
	hashes, err := pk.Objects()
	if err != nil {
		t.Fatal("list pack", err)
	}
	for i, h := range hashes {
		o, err := pk.Extract(h)
		if err != nil {
			typ, data, _ := pk.extract(h)
			t.Logf("%d %+q", typ, data)
			t.Fatal("Extract", h, err)
			break
		}
		if rehash(o) != h {
			t.Errorf("hash mismatch %s %s", rehash(o), h)
		}
		t.Log(h, o.Type())
		t.Log(prettyPrint(o))
		if i > 20 {
			break
		}
	}
}

func prettyPrint(o Object) string {
	switch o := o.(type) {
	case Blob:
		if len(o.Data) < 40 {
			return strconv.Quote(string(o.Data))
		}
		return strconv.Quote(string(o.Data[:40])) + "..."
	case Tree:
		buf := new(bytes.Buffer)
		for _, e := range o.Entries {
			fmt.Fprintf(buf, "%06o %s %s\n",
				gitMode(e.Mode), e.Hash, e.Name)
		}
		return buf.String()
	case Commit:
		buf := new(bytes.Buffer)
		o.WriteTo(buf)
		buf.ReadBytes(0)
		return buf.String()
	}
	panic("impossible")
}

func ExamplePackReader_Extract() {
	base := ".git/objects/pack/pack-bb4afc76654154e3a9f198723ba89873ecb14293"
	fpack, err := os.Open(base + ".pack")
	if err != nil {
		log.Fatal(err)
	}
	fidx, err := os.Open(base + ".idx")
	if err != nil {
		log.Fatal(err)
	}
	defer fpack.Close()
	defer fidx.Close()

	packsize, err1 := fpack.Seek(0, os.SEEK_END)
	idxsize, err2 := fidx.Seek(0, os.SEEK_END)
	if err1 != nil || err2 != nil {
		log.Fatal(err1, err2)
	}
	pk, err := NewPackReader(
		io.NewSectionReader(fpack, 0, packsize),
		io.NewSectionReader(fidx, 0, idxsize))
	if err != nil {
		log.Fatal(err)
	}
	var hash Hash
	hex.Decode(hash[:], []byte("2e16bbf779131a90346eab585e9e5c4736d3aeac"))
	obj, err := pk.Extract(hash)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("%+v", obj)
}
