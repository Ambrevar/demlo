// Copyright © 2013-2016 Pierre Neidhardt <ambrevar@gmail.com>
// Use of this file is governed by the license that can be found in LICENSE.

package main

import (
	"bytes"
	"github.com/mgutz/ansi"
	"io"
	"io/ioutil"
	"log"
	"os"
	"sync"
)

var displayMutex sync.Mutex

// Slogger is a structured logger for terminal logging.
type Slogger struct {
	Debug     *log.Logger
	Info      *log.Logger
	Output    *log.Logger
	Section   *log.Logger
	Warning   *log.Logger
	Error     *log.Logger
	stderrBuf bytes.Buffer
	stdoutBuf bytes.Buffer
}

func newSlogger(debug, color bool) *Slogger {
	sl := Slogger{}
	sl.Debug = log.New(ioutil.Discard, "@@ ", 0)
	sl.Info = log.New(&sl.stderrBuf, ":: ", 0)
	sl.Output = log.New(&sl.stdoutBuf, "", 0)
	sl.Section = log.New(&sl.stderrBuf, "==> ", 0)
	sl.Warning = log.New(&sl.stderrBuf, ":: Warning: ", 0)
	sl.Error = log.New(&sl.stderrBuf, ":: Error: ", 0)

	if debug {
		sl.Debug.SetOutput(&sl.stderrBuf)
	}

	if color {
		sl.Debug.SetPrefix(ansi.Color(sl.Debug.Prefix(), "cyan+b"))
		sl.Info.SetPrefix(ansi.Color(sl.Info.Prefix(), "magenta+b"))
		sl.Section.SetPrefix(ansi.Color(sl.Section.Prefix(), "green+b"))
		sl.Warning.SetPrefix(ansi.Color(sl.Warning.Prefix(), "blue+b"))
		sl.Error.SetPrefix(ansi.Color(sl.Error.Prefix(), "red+b"))
	}

	return &sl
}

// Flush copies the buffers to stderr and stdout and resets the buffers.
func (sl *Slogger) Flush() {
	displayMutex.Lock()
	// Failure on memory copy means fatal error.
	_, _ = io.Copy(os.Stderr, &sl.stderrBuf)
	_, _ = io.Copy(os.Stdout, &sl.stdoutBuf)
	displayMutex.Unlock()

	sl.stderrBuf.Reset()
	sl.stdoutBuf.Reset()
}
