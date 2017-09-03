// Copyright © 2013-2017 Pierre Neidhardt <ambrevar@gmail.com>
// Use of this file is governed by the license that can be found in LICENSE.

package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/yookoala/realpath"
)

// Basename is like filepath.Base but do not strip the trailing slash.
// If 'path' is empty, return the empty string.
func Basename(path string) string {
	if path == "" {
		return ""
	}
	// Throw away volume name.
	path = path[len(filepath.VolumeName(path)):]
	// Find the last element.
	i := len(path) - 1
	for i >= 0 && !os.IsPathSeparator(path[i]) {
		i--
	}
	if i >= 0 {
		path = path[i+1:]
	}
	return path
}

// CopyFile copies the file with path src to dst. The new file is created with
// src permissions minus the fmask. 'dst' must exist and will be clobbered. This
// allows for writing to a tempfile while not suffering from an overwriting race
// condition. Caller is responsible for checking if src!=dst.
func CopyFile(dst, src string) error {
	sf, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sf.Close()

	sstat, err := sf.Stat()
	if err != nil {
		return err
	}
	if !sstat.Mode().IsRegular() {
		return errors.New("not regular file")
	}

	df, err := os.OpenFile(dst, os.O_WRONLY|os.O_TRUNC, sstat.Mode())
	if err != nil {
		return err
	}
	defer df.Close()

	_, err = io.Copy(df, sf)
	return err
}

// readDirNames reads the directory named by dirname and returns
// a sorted list of directory entries.
// This is a copy of 'filepath.readDirNames'.
func readDirNames(dirname string) ([]string, error) {
	f, err := os.Open(dirname)
	if err != nil {
		return nil, err
	}
	names, err := f.Readdirnames(-1)
	f.Close()
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	return names, nil
}

// Same as 'filepath.walk' but the 'path' is changed to its 'realpath' to
// resolve symbolic links and avoid loops.
func realPathWalk(path string, info os.FileInfo, walkFn filepath.WalkFunc, visited map[string]bool) error {
	realPath, err := realpath.Realpath(path)
	if err == nil {

		path = realPath
		if visited[path] {
			return nil
		}
		visited[path] = true

		var realInfo os.FileInfo
		realInfo, err = os.Lstat(path)
		if err == nil {
			info = realInfo
		}
	}

	err = walkFn(path, info, err)
	if err != nil {
		if info.IsDir() && err == filepath.SkipDir {
			return nil
		}
		return err
	}

	if !info.IsDir() {
		return nil
	}

	names, err := readDirNames(path)
	if err != nil {
		return walkFn(path, info, err)
	}

	for _, name := range names {
		filename := filepath.Join(path, name)
		fileInfo, err := os.Lstat(filename)
		if err != nil {
			if err := walkFn(filename, fileInfo, err); err != nil && err != filepath.SkipDir {
				return err
			}
		} else {
			err = realPathWalk(filename, fileInfo, walkFn, visited)
			if err != nil {
				if !fileInfo.IsDir() || err != filepath.SkipDir {
					return err
				}
			}
		}
	}
	return nil
}

// RealPathWalk is like filepath.Walk but follows symlinks.
func RealPathWalk(root string, walkFn filepath.WalkFunc) error {
	info, err := os.Lstat(root)
	if err != nil {
		return walkFn(root, nil, err)
	}
	visited := make(map[string]bool)
	return realPathWalk(root, info, walkFn, visited)
}

// Ext returns the file name extension used by path. The extension is the suffix
// beginning _after_ the final, non-commencing dot in the final element of path;
// it is empty if there is no dot.
func Ext(path string) string {
	if len(path) == 0 {
		return ""
	}
	for i := len(path) - 2; i > 0 && !os.IsPathSeparator(path[i-1]); i-- {
		if path[i] == '.' {
			return path[i+1:]
		}
	}
	return ""
}

// StripExt returns s without its extension.
// Leading dot is included. This is against filepath.Ext() design but conform
// the Ext() function in this package.
func StripExt(s string) string {
	e := Ext(s)
	if len(e) > 0 {
		return s[:len(s)-len(e)-1]
	}
	return s
}

// Random number state.
// We generate random temporary file names so that there's a good
// chance the file doesn't exist yet - keeps the number of tries in
// TempFile to a minimum.
var rand uint32
var randmu sync.Mutex

func reseed() uint32 {
	return uint32(time.Now().UnixNano() + int64(os.Getpid()))
}

func nextSuffix() string {
	randmu.Lock()
	r := rand
	if r == 0 {
		r = reseed()
	}
	r = r*1664525 + 1013904223 // constants from Numerical Recipes
	rand = r
	randmu.Unlock()
	return strconv.Itoa(int(1e9 + r%1e9))[1:]
}

// TempFile is like io/ioutil.TempFile with suffix.
func TempFile(dir, prefix, suffix string) (f *os.File, err error) {
	if dir == "" {
		dir = os.TempDir()
	}

	nconflict := 0
	for i := 0; i < 10000; i++ {
		name := filepath.Join(dir, prefix+nextSuffix()+suffix)
		f, err = os.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
		if os.IsExist(err) {
			if nconflict++; nconflict > 10 {
				randmu.Lock()
				rand = reseed()
				randmu.Unlock()
			}
			continue
		}
		break
	}
	return
}
