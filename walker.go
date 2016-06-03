package main

import (
	"strings"

	"github.com/yookoala/realpath"
)

type walker struct {
	visited map[string]bool
}

func (w *walker) Init() {
	w.visited = map[string]bool{}
}

func (w *walker) Close() {
}

func (w *walker) Run(fr *FileRecord) error {
	if !options.extensions[strings.ToLower(Ext(fr.input.path))] {
		fr.Debug.Printf("Unknown extension '%v'", Ext(fr.input.path))
		return errInputFile
	}
	rpath, err := realpath.Realpath(fr.input.path)
	if err != nil {
		fr.Error.Print("Cannot get real path:", err)
		return errInputFile
	}
	if w.visited[rpath] {
		fr.Debug.Print("Duplicate file")
		return errInputFile
	}

	w.visited[rpath] = true
	return nil
}
