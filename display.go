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
)

var (
	stderrMutex chan bool
	stdoutMutex chan bool
)

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

func init() {
	stderrMutex = make(chan bool, 1)
	stderrMutex <- true
	stdoutMutex = make(chan bool, 1)
	stdoutMutex <- true
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

func (sl *Slogger) Flush() {
	<-stderrMutex
	io.Copy(os.Stderr, &sl.stderrBuf)
	stderrMutex <- true
	sl.stderrBuf.Reset()

	<-stdoutMutex
	io.Copy(os.Stdout, &sl.stdoutBuf)
	stdoutMutex <- true
	sl.stdoutBuf.Reset()
}
