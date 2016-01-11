// This package is not production ready.
package acoustid

// TODO: replace types with MusicBrainz types? Or keep it independent? See how
// much overlaps. Use a design similar to gomusicbrainz.

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"strconv"
)

const (
	// Both release and recording IDs are required by MusicBrainz.
	ACOUSTID_URI    = "http://api.acoustid.org/v2/lookup?client="
	ACOUSTID_LOOKUP = "&meta=recordings+releases+tracks"
)

type Date struct {
	Month int
	Day   int
	Year  int
}

type ReleaseEvent struct {
	Date    Date
	Country string
}

type Track struct {
	Position int
	Artists  []Artist
	ID       string
	Title    string
}

type Medium struct {
	Position    int
	Tracks      []Track
	Track_count int
	Format      string
}

type Release struct {
	Track_count   int
	ReleaseEvents []ReleaseEvent
	Country       string
	Title         string
	Artists       []Artist
	Date          Date
	MediumCount   int
	Mediums       []Medium
	ID            string
}

type Artist struct {
	ID   string
	Name string
}

type Recording struct {
	Releases []Release
	Artists  []Artist
	Duration int
	Title    string
	ID       string
}

type Result struct {
	Recordings []Recording
	Score      float64
	ID         string
}

type Meta struct {
	Results []Result
	Status  string
	Error   struct{ Message string }
}

func Get(acoustIDKey string, fingerprint string, duration int) (metadata Meta, err error) {
	resp, err := http.DefaultClient.Get(ACOUSTID_URI + acoustIDKey + ACOUSTID_LOOKUP + "&duration=" + strconv.Itoa(duration) + "&fingerprint=" + fingerprint)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	buf, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return
	}

	err = json.Unmarshal(buf, &metadata)
	return
}
