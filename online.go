// Copyright © 2013-2016 Pierre Neidhardt <ambrevar@gmail.com>
// Use of this file is governed by the license that can be found in LICENSE.

// TODO: Test how memoization scales with coverCache and tagsCache.
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

	"bitbucket.org/ambrevar/demlo/acoustid"
	"github.com/michiwend/gomusicbrainz"
)

const (
	acoustIDAPIKey = "iOiEFv7y"
	// Threshold above which a key is considered a match for the cache.
	relationThreshold = 0.7
)

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
	releaseIndex = struct {
		v map[AlbumKey]ReleaseID
		sync.RWMutex
	}{v: map[AlbumKey]ReleaseID{}}
	tagsCache = struct {
		v map[ReleaseID]Tags
		sync.RWMutex
	}{v: map[ReleaseID]Tags{}}
	coverCache = struct {
		v map[ReleaseID]Cover
		sync.RWMutex
	}{v: map[ReleaseID]Cover{}}

	errMissingCover = errors.New("cover not found")
	errUnidentAlbum = errors.New("unidentifiable album")
)

// RecordingID is the MusicBrainz ID of a specific track. Different remixes have
// different RecordingIDs.
type RecordingID gomusicbrainz.MBID

// ReleaseID is the MusicBrainz ID of a specific album release. Releases in
// different countries with varying bonus content have different ReleaseIDs.
type ReleaseID gomusicbrainz.MBID

// AlbumKey is used to cluster tracks by album. The key is used in the lookup
// cache.
type AlbumKey struct {
	album       string
	albumartist string
	date        string
}

// Recording holds tag information of the track.
type Recording struct {
	artist   string
	duration int
	title    string
	track    string
}

// Tags holds tag information of an album.
type Tags struct {
	album       string
	albumartist string
	date        string
	recordings  map[RecordingID]Recording
}

// Cover holds the cover and its inputCover description.
type Cover struct {
	picture []byte
	desc    inputCover
}

func init() {
	musicBrainzClient, _ = gomusicbrainz.NewWS2Client("https://musicbrainz.org/ws/2", application, version, URL)
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
	// display.Debug.Print("musicbrainz: release albumartist: ", tags.albumartist)

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
				case 0:
					break
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

				fr.Debug.Printf(`Score: %.4g
%-12s %-7.4g [%v]
%-12s %-7.4g [%v]
%-12s %-7.4g [%v]
%-12s %-7.4g [%v]
%-12s %-7.4g [%v]
Disc %v, Track %v, TrackCount %v: %.4g
`,
					score,
					"Title", relTitle, acoustRecording.Title,
					"Artist", relArtist, dbgArtist,
					"Album", relAlbum, acoustRelease.Title,
					"AlbumArtist", relAlbumArtist, dbgAlbumArtist,
					"Year", relYear, acoustRelease.Date.Year,
					dbgMedium, dbgTrack, dbgTrackCount, relPosition)

				if score > scoreMax {
					fr.Debug.Printf("New max score: %-7.4g\n", score)
					scoreMax = score
					releaseID = ReleaseID(acoustRelease.ID)
					recordingID = RecordingID(acoustRecording.ID)
					if score == 1 {
						// Maximum reached, we can stop here.
						return recordingID, releaseID, nil
					}
				}
			}
		}
	}

	return recordingID, releaseID, nil
}

func queryCover(releaseID ReleaseID) (Cover, error) {
	resp, err := http.DefaultClient.Get("http://coverartarchive.org/release/" + string(releaseID) + "/front")
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

// Return the releaseID corresponding most to tags found in 'input'.
// When the relation is < RELATION_THRESHOLD, return the zero ReleaseID.
func queryIndex(input *inputInfo) (ReleaseID, AlbumKey) {
	album := stringNorm(input.tags["album"])
	if album == "" {
		// If there is no 'album' tag, use the parent folder path. WARNING: This
		// heuristic is not working when tracks of different albums are in the same
		// folder without album tags.
		album = stringNorm(filepath.Dir(input.path))
	}

	albumartist := stringNorm(input.tags["album_artist"])
	date := stringNorm(input.tags["date"])

	var albumKey = AlbumKey{album: album, albumartist: albumartist, date: date}

	// Lookup the release in cache.
	albumMatches := []AlbumKey{}
	albumArtistMatches := []AlbumKey{}
	relMax := 0.0
	var matchKey AlbumKey

	releaseIndex.RLock()
	for key := range releaseIndex.v {
		rel := stringRel(albumKey.album, key.album)
		if rel >= relationThreshold {
			albumMatches = append(albumMatches, key)
		}
	}
	releaseIndex.RUnlock()

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

	releaseIndex.RLock()
	var releaseID = releaseIndex.v[matchKey]
	releaseIndex.RUnlock()

	return releaseID, albumKey
}

func getOnlineTags(fr *FileRecord) (ReleaseID, map[string]string, error) {
	input := &fr.input

	var recordingID RecordingID

	releaseID, albumKey := queryIndex(input)
	fr.Debug.Printf("Album cache Key: %q\n", albumKey)

	tagsCache.RLock()
	tags, ok := tagsCache.v[releaseID]
	tagsCache.RUnlock()
	if !ok {
		// Not cached.

		// Add entry to tagsCache on exit, should the query fail or succeed. If it
		// fails, the entry will be zero, thus allowing to spot dummy entries and
		// avoid querying it again.
		defer func() {
			tagsCache.Lock()
			tagsCache.v[releaseID] = tags
			tagsCache.Unlock()
		}()

		fingerprint, duration, err := fingerprint(input.path)
		if err != nil {
			return "", nil, err
		}

		meta, err := acoustid.Get(acoustIDAPIKey, fingerprint, duration)
		if err != nil {
			return "", nil, err
		}

		recordingID, releaseID, err = queryAcoustID(fr, meta, duration)
		if err != nil {
			return "", nil, err
		}

		// Add releaseID to cache.
		releaseIndex.Lock()
		releaseIndex.v[albumKey] = releaseID
		releaseIndex.Unlock()

		// We use releaseID to identify albums: it is more reliable than the album
		// name in tags.
		tags, err = queryMusicBrainz(releaseID)
		if err != nil {
			return releaseID, nil, err
		}
	}

	if tags.recordings == nil {
		// Dummy entry.

		// The entry is a previously unidentifiable album. Skip it to save time.
		// WARNING: This is a reasonable behaviour, however an album might be
		// partially covered (i.e. missing tracks in MusicBrainz DB).
		return releaseID, nil, errors.New("unidentifiable album")
	}

	fr.Debug.Print("Release ID: ", releaseID)

	if recordingID == "" {
		// Lookup recording in cache. Needed when acoustID was not called.

		// Disc tag is usually wrong or not set, so we browse all discs for tracks
		// with matching durations. Most of the time, there is only 1 disc, hence no
		// additional cost. In case several tracks match, use fuzzy string matching
		// on title, artist and track.
		var matches []RecordingID
		inputDurationFloat, _ := strconv.ParseFloat(input.Format.Duration, 64)
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

	// Loopup the recording over all discs since the disc tag is not reliable.
	recording, ok := tags.recordings[recordingID]
	if !ok {
		return releaseID, nil, errors.New("recording ID absent from cache")
	}

	fr.Debug.Print("Recording ID: ", recordingID)

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

// See 'getOnlineTags' comments.
func getOnlineCover(fr *FileRecord, releaseID ReleaseID) (picture []byte, desc inputCover, err error) {
	input := &fr.input

	var albumKey AlbumKey

	if releaseID == "" {
		releaseID, albumKey = queryIndex(input)
	}

	coverCache.RLock()
	cover, ok := coverCache.v[releaseID]
	coverCache.RUnlock()
	if !ok {
		// Not cached.

		defer func() {
			coverCache.Lock()
			coverCache.v[releaseID] = cover
			coverCache.Unlock()
		}()

		// The releaseID can be known from other caches (tagsCache) while not
		// referenced yet in coverCache. We only need fingerprinting when releaseID
		// is unknown.
		if releaseID == "" {
			fingerprint, duration, err := fingerprint(input.path)
			if err != nil {
				return nil, inputCover{}, err
			}
			meta, err := acoustid.Get(acoustIDAPIKey, fingerprint, duration)
			if err != nil {
				return nil, inputCover{}, err
			}

			_, releaseID, err = queryAcoustID(fr, meta, duration)
			if err != nil {
				return nil, inputCover{}, err
			}
		}

		// Add releaseID to cache.
		releaseIndex.Lock()
		releaseIndex.v[albumKey] = releaseID
		releaseIndex.Unlock()

		cover, err = queryCover(releaseID)
		if err != nil {
			return nil, inputCover{}, err
		}
	}

	if len(cover.picture) == 0 {
		// Dummy entry: The entry that was found was a previously unidentifiable
		// album.
		return nil, inputCover{}, errUnidentAlbum
	}

	return cover.picture, cover.desc, nil
}
