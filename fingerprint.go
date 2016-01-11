// Copyright © 2013-2016 Pierre Neidhardt <ambrevar@gmail.com>
// Use of this file is governed by the license that can be found in LICENSE.

package main

// TODO: Use "github.com/go-fingerprint/fingerprint"? Package seems broken as of 2015.12.01.

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
		return "", 0, errors.New(fmt.Sprintf("fingerprint: %s", stderr.String()))
	}

	var durationSlice []byte

	// Skip file line.
	for i, c := range out {
		if c == '\n' {
			out = out[i+1:]
			break
		}
	}
	// Skip "DURATION=".
	for i, c := range out {
		if c == '=' {
			out = out[i+1:]
			break
		}
	}
	for i, c := range out {
		if c == '\n' {
			durationSlice = out[:i]
			out = out[i+1:]
			break
		}
	}
	// Skip "FINGERPRINT=".
	for i, c := range out {
		if c == '=' {
			out = out[i+1:]
			break
		}
	}
	// Strip trailing newline.
	if out[len(out)-1] == '\n' {
		out = out[:len(out)-1]
	}

	duration, err = strconv.Atoi(string(durationSlice))
	if err != nil {
		return "", 0, err
	}
	return string(out), duration, nil
}
