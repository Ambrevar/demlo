// Copyright © 2013-2016 Pierre Neidhardt <ambrevar@gmail.com>
// Use of this file is governed by the license that can be found in LICENSE.

package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// Like filepath.Base but do not strip the trailing slash. If 'path' is empty,
// return the empty string.
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

	if _, err := io.Copy(df, sf); err != nil {
		return err
	}

	return nil
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

// Returns s without its extension.
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

// Save as io/ioutil.TempFile with suffix.
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
