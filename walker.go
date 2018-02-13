// Copyright © 2013-2018 Pierre Neidhardt <ambrevar@gmail.com>
// Use of this file is governed by the license that can be found in LICENSE.

package main

import (
	"errors"
	"strings"

	"github.com/yookoala/realpath"
)

var errInputFile = errors.New("cannot process input file")

// walker feeds the output channel with files.
// Duplicates are discarded.
type walker struct {
	visited map[string]bool
}

func (w *walker) Init() {
	w.visited = map[string]bool{}
}

func (w *walker) Close() {}

func (w *walker) Run(fr *FileRecord) error {
	if !options.Extensions[strings.ToLower(Ext(fr.input.path))] {
		fr.debug.Printf("Unknown extension '%v'", Ext(fr.input.path))
		return errInputFile
	}
	rpath, err := realpath.Realpath(fr.input.path)
	if err != nil {
		fr.error.Print("Cannot get real path:", err)
		return errInputFile
	}
	if w.visited[rpath] {
		fr.debug.Print("Duplicate file")
		return errInputFile
	}

	w.visited[rpath] = true
	return nil
}
