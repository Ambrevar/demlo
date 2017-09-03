// Copyright © 2013-2017 Pierre Neidhardt <ambrevar@gmail.com>
// Use of this file is governed by the license that can be found in LICENSE.

package main

// TODO: Use "github.com/go-fingerprint/fingerprint"?
// Package seems broken as of 2015.12.01.
// This would be more resilient to upstream library change, e.g. when
// chromaprint 1.4 removed the filename from its output.

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
)

func fingerprint(file string) (fingerprint string, duration int, err error) {
	if _, err := exec.LookPath("fpcalc"); err != nil {
		return "", 0, errors.New("fpcalc not found")
	}

	cmd := exec.Command("fpcalc", file)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		return "", 0, fmt.Errorf("fingerprint: %s", stderr.String())
	}

	// 'out' must of the form:
	// ...
	// DURATION=
	// FINGERPRINT=
	// ...

	for !bytes.HasPrefix(out, []byte("DURATION")) {
		out = out[bytes.IndexByte(out, '\n')+1:]
	}

	var durationOutput []byte = out[bytes.IndexByte(out, '=')+1:]
	durationOutput = durationOutput[:bytes.IndexByte(durationOutput, '\n')]

	for !bytes.HasPrefix(out, []byte("FINGERPRINT")) {
		out = out[bytes.IndexByte(out, '\n')+1:]
	}

	out = out[bytes.IndexByte(out, '=')+1:]
	out = out[:bytes.IndexByte(out, '\n')]

	duration, err = strconv.Atoi(string(durationOutput))
	if err != nil {
		return "", 0, err
	}
	return string(out), duration, nil
}
