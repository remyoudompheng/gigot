// Copyright 2012 Rémy Oudompheng. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package repo

import (
	"testing"
)

func TestRepo(t *testing.T) {
	repo, err := Open("../.git")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%+v", repo)
}
