// Copyright © 2013-2016 Pierre Neidhardt <ambrevar@gmail.com>
// Use of this file is governed by the license that can be found in LICENSE.

package main

import (
	"io/ioutil"
	"testing"

	"bitbucket.org/ambrevar/demlo/cuesheet"
)

const (
	sampleCuesheet    = "cuesheet/testdata/sample.cue"
	scriptCase        = "scripts/30-case.lua"
	scriptPunctuation = "scripts/40-punctuation.lua"
)

func TestFixPunctuation(t *testing.T) {
	input := inputInfo{}
	output := outputInfo{
		Tags: map[string]string{
			"a b": "a_b",
			".a":  ".a",
			"a (": "a(",
			"(a":  "( a",
			"a c": "a 	c",
			"a": "	 a 	",
			"Some i.n.i.t.i.a.l.s.": "Some i.n.i.t.i.a.l.s.",
		},
	}

	buf, err := ioutil.ReadFile(scriptPunctuation)
	if err != nil {
		t.Fatal("Script is not readable", err)
	}

	// Compile scripts.
	L, err := MakeSandbox(nil)
	SandboxCompileScript(L, "punctuation", string(buf))
	if err != nil {
		t.Fatal("Spurious sandbox", err)
	}
	defer L.Close()

	err = RunScript(L, "punctuation", &input, &output)
	if err != nil {
		t.Fatalf("script punctuation: %s", err)
	}

	for want, got := range output.Tags {
		if got != want {
			t.Errorf(`Got "%v", want "%v"`, got, want)
		}
	}
}

func TestTitleCase(t *testing.T) {
	input := inputInfo{}
	output := outputInfo{
		Tags: map[string]string{
			"All Lowercase Words":                     "all lowercase words",
			"All Uppercase Words":                     "ALL UPPERCASE WORDS",
			"All Crazy Case Words":                    "aLl cRaZY cASE WordS",
			"With Common Preps in a CD Into the Box.": "With common preps in a cd INTO the box.",
			"Feat and feat. The Machines.":            "Feat and Feat. the machines.",
			"Unicode Apos´trophe":                     "unicode apos´trophe",
			"...":                                                      "...",
			".'?":                                                      ".'?",
			"I'll Be Ill'":                                             "i'll be ill'",
			"Names Like O'Hara, D’Arcy":                                "Names like o'hara, d’arcy",
			"Names Like McDonald and MacNeil":                          "Names like mcdonald and macneil",
			"Éléanor":                                                  "élÉanor",
			"XIV LIV Xiv Liv. Liv. Xiv.":                               "XIV LIV xiv liv. liv. xiv.",
			"A Start With a Lowercase Constant":                        "a start with a lowercase constant",
			`"A Double Quoted Sentence" and 'One Single Quoted'.`:      `"a double quoted sentence" and 'one single quoted'.`,
			`Another "Double Quoted Sentence", and "A Sentence More".`: `another "double quoted sentence", and "a sentence more".`,
			"Some I.N.I.T.I.A.L.S.":                                    "Some i.n.i.t.i.a.l.s.",
		},
	}

	buf, err := ioutil.ReadFile(scriptCase)
	if err != nil {
		t.Fatal("Script is not readable", err)
	}

	// Compile scripts.
	L, err := MakeSandbox(nil)
	SandboxCompileScript(L, "case", string(buf))
	if err != nil {
		t.Fatal("Spurious sandbox", err)
	}
	defer L.Close()

	err = RunScript(L, "case", &input, &output)
	if err != nil {
		t.Fatalf("script case: %s", err)
	}

	for want, got := range output.Tags {
		if got != want {
			t.Errorf(`Got "%v", want "%v"`, got, want)
		}
	}
}

func TestSentenceCase(t *testing.T) {
	input := inputInfo{}
	output := outputInfo{
		Tags: map[string]string{
			"Capitalized words":               "capitalized words",
			"Machine":                         "machine",
			"Rise of the machines":            "Rise Of The Machines",
			"Chanson d'avant":                 "Chanson D'Avant",
			"Names like o'hara, d’arcy":       "Names LIKE O'HARA, D’ARCY",
			"Names like McDonald and MacNeil": "Names LIKE MCDONALD AND MACNEIL",
			"XIV LIV xiv liv. Liv. Xiv.":      "XIV LIV xiv liv. liv. xiv.",
		},
	}

	buf, err := ioutil.ReadFile(scriptCase)
	if err != nil {
		t.Fatal("Script is not readable", err)
	}

	// Compile scripts.
	L, err := MakeSandbox(nil)
	SandboxCompileScript(L, "case", string(buf))
	if err != nil {
		t.Fatal("Spurious sandbox", err)
	}
	defer L.Close()

	// Set setencecase.
	L.PushBoolean(true)
	L.SetGlobal("scase")

	err = RunScript(L, "case", &input, &output)
	if err != nil {
		t.Fatalf("script case: %s", err)
	}

	for want, got := range output.Tags {
		if got != want {
			t.Errorf(`Got "%v", want "%v"`, got, want)
		}
	}
}

func TestStringNorm(t *testing.T) {
	want := []struct {
		s    string
		norm string
	}{
		{s: "A", norm: "a"},
		{s: "0a", norm: "a"},
		{s: "00a", norm: "a"},
		{s: "a0", norm: "a0"},
		{s: "a.0", norm: "a"},
		{s: "a0a", norm: "a0a"},
		{s: "a.0a", norm: "aa"},
		{s: "10", norm: "10"},
		{s: "01", norm: "1"},
		{s: ".a", norm: "a"},
		{s: "..a", norm: "a"},
	}

	for _, v := range want {
		n := stringNorm(v.s)
		if n != v.norm {
			t.Errorf(`Got "%v", want norm("%v")=="%v"`, n, v.s, v.norm)
		}
	}
}

func TestStringRel(t *testing.T) {
	want := []struct {
		a   string
		b   string
		rel float64
	}{
		{a: "foo", b: "bar", rel: 0.0},
		{a: "foo", b: "foo", rel: 1.0},
		{a: "foobar", b: "foobaz", rel: 1 - float64(1)/float64(6)},
		{a: "", b: "b", rel: 0.0},
		{a: "a", b: "", rel: 0.0},
		{a: "", b: "", rel: 1.0},
		{a: "ab", b: "ba", rel: 0.5},
		{a: "abba", b: "aba", rel: 0.75},
		{a: "aba", b: "abba", rel: 0.75},
		{a: "résumé", b: "resume", rel: 1 - float64(2)/float64(6)},
	}

	for _, v := range want {
		r := stringRel(v.a, v.b)
		if r != v.rel {
			t.Errorf(`Got %v, want rel("%v", "%v")==%v`, r, v.a, v.b, v.rel)
		}
	}
}

func TestFFmpegSplitTimes(t *testing.T) {
	// We need to make up last track's duration: 3 minutes.
	totaltime := float64(17*60 + 4 + 3*60)

	want := []struct {
		track    int
		start    string
		duration string
	}{
		{track: 0, start: "00:00:00.000", duration: "00:06:40.360"},
		{track: 1, start: "00:06:40.360", duration: "00:04:13.640"},
		{track: 3, start: "00:17:04.000", duration: "00:03:00.000"},
		{track: 4, start: "", duration: ""},
		{track: 8, start: "", duration: ""},
	}

	buf, err := ioutil.ReadFile(sampleCuesheet)
	if err != nil {
		panic(err)
	}
	sheet, err := cuesheet.New(string(buf))
	if err != nil {
		panic(err)
	}

	for _, v := range want {
		start, duration := ffmpegSplitTimes(sheet, "Faithless - Live in Berlin (CD1).mp3", v.track, totaltime)
		if start != v.start || duration != v.duration {
			t.Errorf("Got {start: %v, duration: %v}, want {start: %v, duration: %v}", start, duration, v.start, v.duration)
		}
	}
}
