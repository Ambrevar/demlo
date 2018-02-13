// Copyright © 2013-2018 Pierre Neidhardt <ambrevar@gmail.com>
// Use of this file is governed by the license that can be found in LICENSE.

package cuesheet

import (
	"io/ioutil"
	"testing"
)

const (
	SAMPLE_CUESHEET = "testdata/sample.cue"
)

func equalMaps(a, b map[string]string) bool {
	if &a == &b {
		return true
	}
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestNew(t *testing.T) {
	buf, err := ioutil.ReadFile(SAMPLE_CUESHEET)
	if err != nil {
		panic(err)
	}
	sheet, err := New(string(buf))
	if err != nil {
		panic(err)
	}

	want := Cuesheet{
		Header: map[string]string{
			"SOURCE":    "http://en.wikipedia.org/wiki/Cue_sheet_(computing)",
			"GENRE":     "Electronica / trip hop",
			"DATE":      "1998",
			"PERFORMER": "Faithless (Album artist)",
			"TITLE":     "Live in Berlin",
		},
		Files: map[string][]Track{
			"Faithless - Live in Berlin (CD1).mp3": []Track{

				Track{
					Indices: []Time{{0, 0, 0}},
					Tags: map[string]string{
						"TRACK":     "01",
						"TITLE":     "Reverence",
						"PERFORMER": "Faithless",
					},
				},

				Track{
					Indices: []Time{{6, 40, 360}, {6, 42, 360}},
					Tags: map[string]string{
						"TRACK":     "02",
						"TITLE":     "She's My Baby",
						"PERFORMER": "Faithless",
					},
				},

				Track{
					Indices: []Time{{10, 54, 00}},
					Pregap:  Time{0, 2, 0},
					Tags: map[string]string{
						"TRACK":     "03",
						"TITLE":     "Take the Long Way Home",
						"PERFORMER": "Faithless",
					},
				},

				Track{
					Indices: []Time{{17, 04, 00}},
					Tags: map[string]string{
						"TRACK":     "04",
						"TITLE":     "Insomnia",
						"PERFORMER": "Faithless",
					},
				},
			},

			"Faithless - Live in Berlin (CD2).mp3": []Track{

				Track{
					Indices: []Time{{25, 44, 00}},
					Tags: map[string]string{
						"TRACK":     "05",
						"TITLE":     "Bring the Family Back",
						"PERFORMER": "Faithless",
					},
				},

				Track{
					Indices: []Time{{30, 50, 00}},
					Tags: map[string]string{
						"TRACK":     "06",
						"TITLE":     "Salva Mea",
						"PERFORMER": "Faithless",
					},
				},

				Track{
					Indices: []Time{{38, 24, 00}},
					Tags: map[string]string{
						"TRACK":     "07",
						"TITLE":     "Dirty Old Man",
						"PERFORMER": "Faithless",
					},
				},

				Track{
					Indices: []Time{{42, 35, 00}},
					Tags: map[string]string{
						"TRACK":     "08",
						"TITLE":     "God Is a DJ",
						"PERFORMER": "Faithless",
					},
				},
			},
		},
	}

	if !equalMaps(sheet.Header, want.Header) {
		t.Errorf("Got %q, want %q", sheet.Header, want.Header)
	}
	if len(sheet.Files) != len(want.Files) {
		t.Errorf("Got len(.Files)==%v, want len(.Files)==%v", len(sheet.Files), len(want.Header))
	}

	for file, tracks := range sheet.Files {
		if _, ok := want.Files[file]; !ok {
			t.Errorf("Got unexpected %v file", file)
		}
		wantedTracks := want.Files[file]
		for pos, track := range tracks {
			if !equalMaps(track.Tags, wantedTracks[pos].Tags) {
				t.Errorf("Got %q, want %q", track.Tags, wantedTracks[pos].Tags)
			}
			if track.Pregap != wantedTracks[pos].Pregap {
				t.Errorf("Got pregap %v, want %v", track.Pregap, wantedTracks[pos].Pregap)
			}
			if track.Postgap != wantedTracks[pos].Postgap {
				t.Errorf("Got postgap %v, want %v", track.Postgap, wantedTracks[pos].Postgap)
			}

			if len(track.Indices) != len(wantedTracks[pos].Indices) {
				t.Errorf("Got len(.Indices)==%v, want len(.Indices)==%v", len(track.Indices), len(wantedTracks[pos].Indices))
			}

			for k, index := range track.Indices {
				if index != wantedTracks[pos].Indices[k] {
					t.Errorf("Got index %v, want %v", index, wantedTracks[pos].Indices[k])
				}
			}
		}
	}
}
