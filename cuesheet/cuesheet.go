// Copyright © 2013-2016 Pierre Neidhardt <ambrevar@gmail.com>
// Use of this file is governed by the license that can be found in LICENSE.

// TODO: Write documentation.

// TODO: Cuesheet library alternative:
// https://godoc.org/github.com/vchimishuk/chub/src/cue.
// At first glance: more compliant to the specs, slower and less robust against
// erroneous cuesheets. Issues: does not close file, GPL3, does not parse from
// buffer, "recieve" typo, suboptimal regexp.

/*
Cuesheet times are specified in the 'mm:ss:ff' format, where 'mm' are minutes,
'ss' seconds, and 'ff' frames. There are 75 frames in 1 second. Time is stored
in {Min, Sec, Msec}.
*/
package cuesheet

import (
	"bufio"
	"bytes"
	"errors"
	"regexp"
	"strconv"
)

var (
	reFile    = regexp.MustCompile(`^\s*FILE\s+"?([^"]+)"?`)
	reIndex   = regexp.MustCompile(`^\s*INDEX\s*\d+\s+(\d\d):(\d\d):(\d\d)`)
	rePostgap = regexp.MustCompile(`^\s*POSTGAP\s+(\d\d):(\d\d):(\d\d)`)
	rePregap  = regexp.MustCompile(`^\s*PREGAP\s+(\d\d):(\d\d):(\d\d)`)
	reTag     = regexp.MustCompile(`^\s*(?:REM\b)?\s*(\S+)\s+"?([^"]+)"?`)
	reTrack   = regexp.MustCompile(`^\s*TRACK\s+(\d+)`)
)

type Time struct {
	Min  int
	Sec  int
	Msec int
}

type Track struct {
	Tags    map[string]string
	Indices []Time
	Pregap  Time
	Postgap Time
}

type Cuesheet struct {
	Header map[string]string
	Files  map[string][]Track
}

// We do not take a path as argument since cuesheets can be found in tags.
func New(cuesheet string) (Cuesheet, error) {
	var sheet Cuesheet
	if cuesheet == "" {
		return sheet, errors.New("empty cuesheet")
	}

	// TODO: Reader/Scanner is a bit heavy. Use simpler parser, e.g. with
	// 'strings' functions.
	b := bytes.NewReader([]byte(cuesheet))
	s := bufio.NewScanner(b)

	header := true
	file := ""

	for s.Scan() {
		match := reFile.FindStringSubmatch(s.Text())
		if len(match) != 0 {
			header = true
			file = match[1]
			continue
		}

		if header {
			match = reTrack.FindStringSubmatch(s.Text())
			if len(match) != 0 {
				header = false
			} else {

				match = reTag.FindStringSubmatch(s.Text())
				if len(match) != 0 {
					if len(match[2]) > 0 {

						if sheet.Header == nil {
							sheet.Header = make(map[string]string)
						}
						sheet.Header[match[1]] = match[2]
					}
					continue
				}
			}
		}

		// After header.

		match = reTrack.FindStringSubmatch(s.Text())
		if len(match) != 0 {
			if sheet.Files == nil {
				sheet.Files = make(map[string][]Track)
			}
			spec := Track{}
			if spec.Tags == nil {
				spec.Tags = make(map[string]string)
			}
			spec.Tags["TRACK"] = match[1]
			sheet.Files[file] = append(sheet.Files[file], spec)
			continue
		}

		// From here we can safely assume that sheet.Files[file] is initialized.
		trackPos := len(sheet.Files[file]) - 1

		match = reIndex.FindStringSubmatch(s.Text())
		if len(match) != 0 {
			min, _ := strconv.Atoi(match[1])
			sec, _ := strconv.Atoi(match[2])
			frames, _ := strconv.Atoi(match[3])
			msec := int(1000 * float64(frames) / 75)
			sheet.Files[file][trackPos].Indices = append(sheet.Files[file][trackPos].Indices, Time{Min: min, Sec: sec, Msec: msec})
			continue
		}

		match = rePregap.FindStringSubmatch(s.Text())
		if len(match) != 0 {
			min, _ := strconv.Atoi(match[1])
			sec, _ := strconv.Atoi(match[2])
			frames, _ := strconv.Atoi(match[3])
			msec := int(1000 * float64(frames) / 75)
			sheet.Files[file][trackPos].Pregap = Time{Min: min, Sec: sec, Msec: msec}
			continue
		}

		match = rePostgap.FindStringSubmatch(s.Text())
		if len(match) != 0 {
			min, _ := strconv.Atoi(match[1])
			sec, _ := strconv.Atoi(match[2])
			frames, _ := strconv.Atoi(match[3])
			msec := int(1000 * float64(frames) / 75)
			sheet.Files[file][trackPos].Postgap = Time{Min: min, Sec: sec, Msec: msec}
			continue
		}

		// Should be last.
		match = reTag.FindStringSubmatch(s.Text())
		if len(match) != 0 {
			if len(match[2]) > 0 {
				sheet.Files[file][trackPos].Tags[match[1]] = match[2]
			}
			continue
		}

		return Cuesheet{nil, nil}, errors.New("cannot parse " + s.Text())
	}

	return sheet, nil
}
