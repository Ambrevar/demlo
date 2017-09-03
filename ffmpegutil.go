// Copyright © 2013-2017 Pierre Neidhardt <ambrevar@gmail.com>
// Use of this file is governed by the license that can be found in LICENSE.

package main

import (
	"fmt"

	"github.com/ambrevar/demlo/cuesheet"
)

/* ffmpegSplitTimes returns the starting time and duration (in FFmpeg CLI format) of a track in a multi-track file.

Since a cuesheet does not contain the total duration, we cannot infere last
track's duration only from the sheet. We need to pass it as parameter.

Total duration is a floating value; second is the unit.

First track is track 0.

TODO: We ignore Indices beyond the first one. As a result, it may include
silences. But always skipping the first index (if there is a second one) might
not be he desired result either. Finally, there could be more than 2 indices,
even thought I have no clue to what use. Rationale needed.
*/
func ffmpegSplitTimes(sheet cuesheet.Cuesheet, file string, track int, totalduration float64) (start, duration string) {

	var totalmsec int

	if sheet.Files[file] == nil {
		return "", ""
	}
	if track >= len(sheet.Files[file]) {
		return "", ""
	}
	if track < len(sheet.Files[file])-1 {
		// Not last track
		min := sheet.Files[file][track+1].Indices[0].Min
		sec := sheet.Files[file][track+1].Indices[0].Sec
		msec := sheet.Files[file][track+1].Indices[0].Msec
		totalmsec = (1000*60*min + 1000*sec + msec)
	} else {
		totalmsec = int(totalduration * 1000)
	}

	min := sheet.Files[file][track].Indices[0].Min
	sec := sheet.Files[file][track].Indices[0].Sec
	msec := sheet.Files[file][track].Indices[0].Msec

	diff := totalmsec - (1000*60*min + 1000*sec + msec)

	dmsec := diff % 1000
	diff /= 1000
	dsec := diff % 60
	diff /= 60
	dmin := diff % 60
	dhour := diff / 60

	hour := min / 60
	min = min % 60

	return fmt.Sprintf("%02d:%02d:%02d.%03d", hour, min, sec, msec),
		fmt.Sprintf("%02d:%02d:%02d.%03d", dhour, dmin, dsec, dmsec)
}
