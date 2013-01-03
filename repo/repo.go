// Copyright 2012 Rémy Oudompheng. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// package repo provides an interface to access Git repositories.
package repo

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io/ioutil"
	"path/filepath"

	"github.com/remyoudompheng/gigot/objects"
)

func Open(dirname string) (*Repo, error) {
	headfiles, err := ioutil.ReadDir(filepath.Join(dirname, "refs/heads"))
	if err != nil {
		return nil, err
	}
	repo := new(Repo)
	repo.Path = dirname
	for _, h := range headfiles {
		s, err := ioutil.ReadFile(filepath.Join(dirname, "refs/heads", h.Name()))
		if err != nil {
			return nil, err
		}
		ref := Ref{Name: h.Name()}
		s = bytes.TrimSpace(s)
		n, err := hex.Decode(ref.Id[:], s)
		if err != nil {
			return nil, err
		}
		if n < 20 {
			return nil, errTruncatedHead
		}
		repo.Branches = append(repo.Branches, ref)
	}
	return repo, nil
}

var (
	errTruncatedHead = errors.New("gigot: head info truncated")
)

type Repo struct {
	Path     string
	Branches []Ref
}

type Ref struct {
	Name string
	Id   objects.Hash
}
