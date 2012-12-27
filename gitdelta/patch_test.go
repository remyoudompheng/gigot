// Copyright 2012 Rémy Oudompheng. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gitdelta

import (
	"bytes"
	"io/ioutil"
	"testing"
)

func TestPatch(t *testing.T) {
	mustRead := func(n string) []byte {
		s, err := ioutil.ReadFile(n)
		if err != nil {
			t.Fatalf("reading %s: %s", n, err)
		}
		return s
	}
	s1 := mustRead("testdata/golden.old")
	s2 := mustRead("testdata/golden.new")
	// Patch created using the test-delta utility from git sources.
	p := mustRead("testdata/golden.delta")

	s, err := Patch(s1, p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(s, s2) {
		t.Errorf("difference: got %q, expect %q", s, s2)
	}
}
