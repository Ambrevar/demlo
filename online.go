// Copyright © 2013-2017 Pierre Neidhardt <ambrevar@gmail.com>
// Use of this file is governed by the license that can be found in LICENSE.

// TODO: Test how memoization scales with caches.
// TODO: Check if proxy env variables are taken into account for AcoustID and musicbrainz.
// TODO: Add CLI option to select the online entry to tag from.
// TODO: Add CLI option to select the tolerance to tag approximation when online-tagging:
// 0: always use acoustid;
// 1: check album, artist and date;
// 2: check album and	artist;
// 3: check album only;
// 4: use only 1 album.

// Fetch cover and tags online.
//
// We cache results to minimize network queries. A file is uniquely identified
// by its ReleaseID. This ReleaseID can be used to fetch album tags and cover.
// The RecordingID (ID of a track within an album) can be used to select the
// proper tags of a track within an album.
//
// The cover query and tags query are independent processes, however they both
// need to fingerprint the file to query the ReleaseID if not cached. To save
// some network queries, we query tags first and re-use its ReleaseID for the
// cover query.
//
// To save some fingerprinting between files of the same album, we index
// ReleaseIDs by {album, albumartist, date} so that we can query them with tags
// only.

package main

import (
	"bytes"
	"crypto/md5"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io/ioutil"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"

	"github.com/ambrevar/demlo/acoustid"
	"github.com/michiwend/gomusicbrainz"
)

const acoustIDAPIKey = "iOiEFv7y"

var (
	// gomusicbrainz recreates an HTTP transport stream on every connection, thus
	// inhibiting the benefit of keep-alive connections. TODO: report upstream.
	musicBrainzClient *gomusicbrainz.WS2Client

	reCover = regexp.MustCompile(`<div class="cover-art"><img src="([^"]+)"`)
	reYear  = regexp.MustCompile(`\d\d\d\d+`)
	reTrack = regexp.MustCompile(`\d+`)

	// Map tags to releaseID for future files. This spares some acoustID queries. We
	// index releaseIDs by {album, albumartist, year} to avoid confusion on albums
	// with the same name.
	// Caches are global and access should be mutually exclusive among goroutines.

	// TODO: Should this be a map[AlbumKey][]Release in case several tracks have
	// the same AlbumKey but refer to a different album?
	releaseIDCache = ReleaseIDCache{v: map[AlbumKey]*releaseIDEntry{}}
	tagsCache      = TagsCache{v: map[ReleaseID]*tagsEntry{}}
	coverCache     = CoverCache{v: map[ReleaseID]*coverEntry{}}

	errMissingCover = errors.New("cover not found")
	errUnidentAlbum = errors.New("unidentifiable album")
)

func init() {
	musicBrainzClient, _ = gomusicbrainz.NewWS2Client("https://musicbrainz.org/ws/2", application, version, URL)
}

// AlbumKey is used to cluster tracks by album. The key is used in the lookup
// cache.
type AlbumKey struct {
	album       string
	albumartist string
	date        string
}

func makeAlbumKey(input *inputInfo) AlbumKey {
	album := stringNorm(input.tags["album"])
	if album == "" {
		// If there is no 'album' tag, use the parent folder path. WARNING: This
		// heuristic is not working when tracks of different albums are in the same
		// folder without album tags.
		album = stringNorm(filepath.Dir(input.path))
	}

	albumartist := stringNorm(input.tags["album_artist"])
	date := stringNorm(input.tags["date"])

	return AlbumKey{album: album, albumartist: albumartist, date: date}
}

// Recording holds tag information of the track.
type Recording struct {
	artist   string
	duration int
	title    string
	track    string
}

// RecordingID is the MusicBrainz ID of a specific track. Different remixes have
// different RecordingIDs.
type RecordingID gomusicbrainz.MBID

// ReleaseID is the MusicBrainz ID of a specific album release. Releases in
// different countries with varying bonus content have different ReleaseIDs.
type ReleaseID gomusicbrainz.MBID

type releaseIDEntry struct {
	releaseID ReleaseID
	ready     chan struct{}
}

// ReleaseIDCache allows to retrieve the ReleaseID of track for a known album,
// using its AlbumKey, that is, some track metadata. It saves the need for
// fingerprinting and the AcoustID query.
// The cache can be accessed concurrently. The chan is an memoization idiom that
// allows for duplicate suppression in queries.
type ReleaseIDCache struct {
	v map[AlbumKey]*releaseIDEntry
	sync.Mutex
}

// Return the releaseID corresponding most to tags found in 'input'.
// When the relation is < RELATION_THRESHOLD, return the zero ReleaseID "".
// The RecordingID comes for free when the release ID is queried, so we might
// just return it as well.
func (c *ReleaseIDCache) get(albumKey AlbumKey, fr *FileRecord) (ReleaseID, RecordingID, error) {
	var recordingID RecordingID
	var err error

	c.Lock()
	e, exactMatch := c.fuzzyMatch(albumKey)
	if e == nil {
		fr.debug.Print("Fetch new releaseID for uncached albumKey")

		e = &releaseIDEntry{ready: make(chan struct{})}
		c.v[albumKey] = e
		c.Unlock()

		defer func() {
			close(e.ready)
		}()

		fingerprint, duration, err := fingerprint(fr.input.path)
		if err != nil {
			return "", "", err
		}
		meta, err := acoustid.Get(acoustIDAPIKey, fingerprint, duration)
		if err != nil {
			return "", "", err
		}
		var releaseID ReleaseID
		recordingID, releaseID, err = queryAcoustID(fr, meta, duration)
		if err != nil {
			return "", "", err
		}

		// Only set e.releaseID when all the queries succeed to guarantee
		// e.releaseID is either zero or a valid release ID.
		e.releaseID = releaseID
	} else {
		c.Unlock()
		fr.debug.Print("Wait for cached releaseID")
		<-e.ready

		if !exactMatch {
			// If a non-exact match was found, the key is not cache at this point. Add
			// it to reduce drifting away and to speed-up possible future exact
			// matches.
			c.Lock()
			fr.debug.Print("Add non-exact match to release cache")
			ready := make(chan struct{})
			c.v[albumKey] = &releaseIDEntry{releaseID: e.releaseID, ready: ready}
			close(ready)
			c.Unlock()
		}
	}

	return e.releaseID, recordingID, err
}

// Warning: not concurrent-safe, caller must mutex the call.
// We look for exact matches first to speed-up the process.
func (c *ReleaseIDCache) fuzzyMatch(albumKey AlbumKey) (r *releaseIDEntry, exactMatch bool) {
	r = c.v[albumKey]
	if r != nil {
		return r, true
	}

	// Threshold above which a key is considered a match for the cache.
	const relationThreshold = 0.7

	// Lookup the release in cache.
	albumMatches := []AlbumKey{}
	albumArtistMatches := []AlbumKey{}
	relMax := 0.0
	var matchKey AlbumKey

	for key := range c.v {
		rel := stringRel(albumKey.album, key.album)
		if rel >= relationThreshold {
			albumMatches = append(albumMatches, key)
		}
	}

	for _, key := range albumMatches {
		rel := stringRel(albumKey.albumartist, key.albumartist)
		if rel >= relationThreshold {
			albumArtistMatches = append(albumArtistMatches, key)
		}
	}

	for _, key := range albumArtistMatches {
		rel := stringRel(albumKey.date, key.date)
		if rel >= relationThreshold && rel > relMax {
			relMax = rel
			matchKey = key
		}
	}

	return c.v[matchKey], false
}

// Tags holds tag information of an album.
type Tags struct {
	album       string
	albumartist string
	date        string
	recordings  map[RecordingID]Recording
}

type tagsEntry struct {
	tags  Tags
	ready chan struct{}
}

// TagsCache is used to retrieve tags of a track for a known album. It saves a
// MusicBrainz query.
// See ReleaseIDCache.
type TagsCache struct {
	v map[ReleaseID]*tagsEntry
	sync.Mutex
}

// When adding an entry to tagsCache, the function will end prematurely on
// error, leaving the 'tags' structure empty (zero value). It allows for
// spotting dummy entries during future queries and avoid running into errors
// again.
func (c *TagsCache) get(releaseID ReleaseID, albumKey AlbumKey, fr *FileRecord) (*Tags, error) {
	var err error

	c.Lock()
	e := c.v[releaseID]
	if e == nil {
		fr.debug.Print("Fetch new tags for uncached releaseID")

		e = &tagsEntry{ready: make(chan struct{})}
		c.v[releaseID] = e
		c.Unlock()

		// We use releaseID to identify albums: it is more reliable than the album
		// name in tags.
		e.tags, err = queryMusicBrainz(releaseID)
		close(e.ready)
	} else {
		c.Unlock()
		fr.debug.Print("Wait for cached tags")
		<-e.ready
	}

	return &e.tags, err
}

// Cover holds the cover and its inputCover description.
type Cover struct {
	picture []byte
	desc    inputCover
}

type coverEntry struct {
	cover Cover
	ready chan struct{}
}

// CoverCache is like TagsCache.
// Also see ReleaseIDCache.
type CoverCache struct {
	v map[ReleaseID]*coverEntry
	sync.Mutex
}

// See TagsCache.Get documentation.
func (c *CoverCache) get(releaseID ReleaseID, fr *FileRecord) (*Cover, error) {
	var err error

	c.Lock()
	e := c.v[releaseID]
	if e == nil {
		fr.debug.Print("Fetch new cover for uncached releaseID")

		e = &coverEntry{ready: make(chan struct{})}
		c.v[releaseID] = e
		c.Unlock()

		e.cover, err = queryCover(releaseID)
		close(e.ready)
	} else {
		c.Unlock()
		fr.debug.Print("Wait for cached cover")
		<-e.ready
	}

	return &e.cover, err
}

// MusicBrainz returns 2 artist names per recording. They are stored in the NameCredit struct:
// type NameCredit struct {
// 	Name string   `xml:"name"` // Not implemented!
// 	Artist Artist `xml:"artist"`
// }
// 'Name' is the name as showed on the official album case.
// 'Artist' links to the official artist name.
// As of 2015/12/06, gomusicbrainz does not implement 'name'. TODO: Report upstream? The official name is better anyways.
func queryMusicBrainz(releaseID ReleaseID) (Tags, error) {
	mbRelease, err := musicBrainzClient.LookupRelease(gomusicbrainz.MBID(releaseID), "recordings", "artist-credits")
	if err != nil {
		return Tags{}, errors.New("MusicBrainz: " + err.Error())
	}

	// Store the releaseID for cover retrieval when cache is used (and not
	// AcoustID).
	tags := Tags{date: strconv.Itoa(mbRelease.Date.Time.Year()), album: mbRelease.Title}
	tags.recordings = make(map[RecordingID]Recording)

	if len(mbRelease.ArtistCredit.NameCredits) > 0 {
		tags.albumartist = mbRelease.ArtistCredit.NameCredits[0].Artist.Name
	}

	// TODO: Add more MusicBrainz debug info.
	// fr.debug.Print("musicbrainz: release albumartist: ", tags.albumartist)

	for _, entry := range mbRelease.Mediums {
		for _, v := range entry.Tracks {

			rec := Recording{
				track:    v.Number,
				title:    v.Recording.Title,
				duration: v.Recording.Length,
			}

			if len(v.Recording.ArtistCredit.NameCredits) > 0 {
				rec.artist = v.Recording.ArtistCredit.NameCredits[0].Artist.Name
			}

			if v.Recording.Length == 0 {
				rec.duration = v.Length
			}

			tags.recordings[RecordingID(v.Recording.ID)] = rec
		}
	}

	return tags, nil
}

func queryAcoustID(fr *FileRecord, meta acoustid.Meta, duration int) (recordingID RecordingID, releaseID ReleaseID, err error) {
	// Shorthand.
	tags := fr.input.tags

	if meta.Status == "error" {
		return "", "", errors.New("AcoustID: " + meta.Error.Message)
	}

	disc, err := strconv.Atoi(tags["disc"])
	if err != nil {
		// If 'disc' is unspecified, assume it is the first disc.
		disc = 1
	}

	albumartist := stringNorm(tags["album_artist"])
	album := stringNorm(tags["album"])
	title := stringNorm(tags["title"])
	artist := stringNorm(tags["artist"])
	date := reYear.FindString(tags["date"])
	track, err := strconv.Atoi(reTrack.FindString(tags["track"]))
	if err != nil {
		track = 0
	}

	scoreMax := 0.0

	for _, acoustResult := range meta.Results {
		for _, acoustRecording := range acoustResult.Recordings {

			relArtist := 0.0
			dbgArtist := ""
			for _, v := range acoustRecording.Artists {
				rel := stringRel(stringNorm(v.Name), artist)
				if rel > relArtist {
					relArtist = rel
					dbgArtist = v.Name
					if relArtist == 1 {
						break
					}
				}
			}

			relTitle := stringRel(stringNorm(acoustRecording.Title), title)

			// Some recordings have the same tags but different durations, e.g. for
			// re-engineered releases.
			relDuration := float64(duration - acoustRecording.Duration)
			if relDuration < 0 {
				relDuration = -relDuration
			}
			relDuration = 1 - relDuration/float64(duration)

			for _, acoustRelease := range acoustRecording.Releases {
				relAlbum := stringRel(stringNorm(acoustRelease.Title), album)

				relAlbumArtist := 0.0
				dbgAlbumArtist := ""
				for _, v := range acoustRelease.Artists {
					rel := stringRel(stringNorm(v.Name), albumartist)
					if rel > relAlbumArtist {
						relAlbumArtist = rel
						dbgAlbumArtist = v.Name
						if relAlbumArtist == 1 {
							break
						}
					}
				}

				relYear := 0.0
				year, err := strconv.Atoi(date)
				if err != nil {
					year = 0
				}

				switch acoustRelease.Date.Year {
				case year:
					relYear = 1
				case year - 1, year + 1:
					// Arbitrary distance: when an album is released around the
					// beginning/end of the year Y, the publishing date that is reported
					// can vary between Y-1 and Y+1.
					relYear = 0.75
				}

				relPosition := 0.0
				dbgMedium := 0
				dbgTrack := 0
				dbgTrackCount := 0
				for _, medium := range acoustRelease.Mediums {
					relDisc := 0.0
					if medium.Position == disc {
						dbgMedium = medium.Position
						relDisc = 1
					}

					// The more tracks, the better: some releases have bonus tracks.
					relTrackCount := 0.0
					if medium.Track_count != 0 {
						relTrackCount = 1 - 1/float64(medium.Track_count)
					}

					relTrack := 0.0
					for _, v := range medium.Tracks {
						if v.Position == track {
							dbgTrack = v.Position
							relTrack = 1
							break
						}
					}

					score := (1*relDisc + 1*relTrack + 1*relTrackCount) / 3
					if score > relPosition {
						relPosition = score
						dbgTrackCount = medium.Track_count
						if relPosition == 1 {
							break
						}
					}
				}

				// Score heuristic from 0 to 1.
				// When 'title' and 'artist' fully match, there is no better result. Thus this accounts for >50%.
				// In case of tie, album and album_artist determines the best subresult. This accounts for >25%.
				// In case of tie, position has more weight than year and duration.
				score := (26*relTitle + 25*relArtist + 13*relAlbumArtist + 13*relAlbum + 9*relPosition + 7*relYear + 7*relDuration) / 100

				if score > scoreMax {
					fr.debug.Printf("Score: %.4g (new max)", score)
					scoreMax = score
					releaseID = ReleaseID(acoustRelease.ID)
					recordingID = RecordingID(acoustRecording.ID)
				} else {
					fr.debug.Printf("Score: %.4g", score)
				}
				fr.debug.Printf(`
%-12s %-7.4g [%v]
%-12s %-7.4g [%v]
%-12s %-7.4g [%v]
%-12s %-7.4g [%v]
%-12s %-7.4g [%v]
Disc %v, Track %v, TrackCount %v: %.4g
`,
					"Title", relTitle, acoustRecording.Title,
					"Artist", relArtist, dbgArtist,
					"Album", relAlbum, acoustRelease.Title,
					"AlbumArtist", relAlbumArtist, dbgAlbumArtist,
					"Year", relYear, acoustRelease.Date.Year,
					dbgMedium, dbgTrack, dbgTrackCount, relPosition)

				if score == 1 {
					// Maximum reached, we can stop here.
					return recordingID, releaseID, nil
				}
			}
		}
	}

	return recordingID, releaseID, nil
}

func queryCover(releaseID ReleaseID) (Cover, error) {
	resp, err := http.DefaultClient.Get("http://coverartarchive.org/release/" + string(releaseID) + "/front")
	if err != nil {
		return Cover{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		resp.Body.Close()
		resp, err = http.DefaultClient.Get("https://musicbrainz.org/release/" + string(releaseID))

		if err != nil {
			return Cover{}, err
		}
		if resp.StatusCode != 200 {
			return Cover{}, errMissingCover
		}
		buf, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return Cover{}, err
		}
		resp.Body.Close()

		// TODO: HTML parsing with regexps is fragile. Sadly, the HTML tokenizer is
		// not part of the standard library. The choice lies between the cost of
		// another dependency and a simple regexp.
		matches := reCover.FindSubmatch(buf)
		if matches == nil {
			return Cover{}, errMissingCover
		}
		uri := string(matches[1])

		resp, err = http.DefaultClient.Get(uri)
		if err != nil {
			return Cover{}, err
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			return Cover{}, errMissingCover
		}
	}

	cover := Cover{}

	cover.picture, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return cover, err
	}

	reader := bytes.NewBuffer(cover.picture)
	config, format, err := image.DecodeConfig(reader)
	if err != nil {
		return cover, err
	}

	hi := len(cover.picture)
	if hi > coverChecksumBlock {
		hi = coverChecksumBlock
	}
	checksum := fmt.Sprintf("%x", md5.Sum(cover.picture[:hi]))

	cover.desc = inputCover{format: format, width: config.Width, height: config.Height, checksum: checksum}

	return cover, nil
}

// GetOnlineTags retrieves tags from MusicBrainz.
// It also returns the ReleaseID of the track which can be used with
// 'GetOnlineCover' to speed up the process.
func GetOnlineTags(fr *FileRecord) (ReleaseID, map[string]string, error) {
	fr.debug.Printf("Get tags")

	var recordingID RecordingID
	input := &fr.input

	albumKey := makeAlbumKey(input)
	// recordingID will be set only when releaseID is queried online. When hitting
	// the cache, the recordingID is missing so we need to infere its value from
	// the heuristic below.
	releaseID, recordingID, err := releaseIDCache.get(albumKey, fr)
	if err != nil {
		return "", nil, err
	}
	fr.debug.Printf("albumKey = %q", albumKey)

	tags, err := tagsCache.get(releaseID, albumKey, fr)
	if err != nil {
		return releaseID, nil, err
	}

	if tags.recordings == nil {
		// The entry is a previously unidentifiable album. Skip it to save time.
		// WARNING: This is a reasonable behaviour, however an album might be
		// partially covered (i.e. missing tracks in MusicBrainz DB).
		return releaseID, nil, errUnidentAlbum
	}

	fr.debug.Printf("releaseID = %q", releaseID)

	if recordingID == "" {
		// Lookup recording in cache. Needed when acoustID was not called.

		// Disc tag is usually wrong or not set, so we browse all discs for tracks
		// with matching durations. Most of the time, there is only 1 disc, hence no
		// additional cost. In case several tracks match, use fuzzy string matching
		// on title, artist and track.
		var matches []RecordingID
		inputDurationFloat, _ := strconv.ParseFloat(fr.Format.Duration, 64)
		inputDuration := int(inputDurationFloat * 1000)

		title := stringNorm(input.tags["title"])
		if title == "" {
			// If there is no 'title' tag, use the file name.
			title = stringNorm(filepath.Base(input.path))
		}

		artist := stringNorm(input.tags["artist"])

		track := stringNorm(input.tags["track"])
		if track == "" {
			// If there is no 'track' tag, use the first number in the file name.
			track = reTrack.FindString(filepath.Base(input.path))
		}

		for k, v := range tags.recordings {
			// If duration score does not fit +/- 4 seconds, reject.
			if inputDuration-v.duration < 4000 &&
				inputDuration-v.duration > -4000 {
				matches = append(matches, k)
			}
		}

		if len(matches) == 1 {
			recordingID = matches[0]
		} else if len(matches) > 1 {
			scoreMax := 0.0
			for _, id := range matches {
				v := tags.recordings[id]
				score := 0.0
				// Give more weight to the title than to the track since track numbers
				// are easily mixed up.
				score += 3 * stringRel(stringNorm(v.title), title)
				score += 2 * stringRel(stringNorm(v.artist), artist)
				score += 1 * stringRel(stringNorm(v.track), track)
				if score > scoreMax {
					scoreMax = score
					recordingID = id
				}
			}
		}
	}

	// Lookup the recording over all discs since the disc tag is not reliable.
	recording, ok := tags.recordings[recordingID]
	if !ok {
		return releaseID, nil, errors.New("recording ID absent from cache")
	}

	fr.debug.Printf("recordingID = %q", recordingID)

	// At this point, 'release' and 'recording' must be properly set.
	var result map[string]string
	result = make(map[string]string)
	result["album"] = tags.album
	result["album_artist"] = tags.albumartist
	result["artist"] = recording.artist
	result["date"] = tags.date
	result["title"] = recording.title
	result["track"] = recording.track

	return releaseID, result, nil
}

// GetOnlineCover is like GetOnlineTags.
func GetOnlineCover(fr *FileRecord, releaseID ReleaseID) (picture []byte, desc inputCover, err error) {
	fr.debug.Printf("Get cover (releaseID = %q)", releaseID)

	input := &fr.input

	// The releaseID can be known from other caches (tagsCache) while not
	// referenced yet in CoverCache. We only need fingerprinting when releaseID is
	// unknown.
	if releaseID == "" {
		var albumKey = makeAlbumKey(input)
		fr.debug.Printf("albumKey = %q", albumKey)
		releaseID, _, err = releaseIDCache.get(albumKey, fr)
		if err != nil {
			return nil, inputCover{}, err
		}
		fr.debug.Printf("releaseID = %q", releaseID)
	}

	cover, err := coverCache.get(releaseID, fr)
	if err != nil {
		return nil, inputCover{}, err
	}

	if len(cover.picture) == 0 {
		// Dummy entry: The entry that was found was a previously unidentifiable
		// album.
		return nil, inputCover{}, errUnidentAlbum
	}

	return cover.picture, cover.desc, nil
}
