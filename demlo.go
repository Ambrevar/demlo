// Copyright © 2013-2016 Pierre Neidhardt <ambrevar@gmail.com>
// Use of this file is governed by the license that can be found in LICENSE.

// TODO: Add shell auto-completion file.
// TODO: Allow for embedding covers. Have a look at:
// * mp4art (libmp4v2): mp4art --add cover.jpg track.m4a
// * vorbiscomment (vorbis-tools)
// * beets
// * http://superuser.com/questions/169151/embed-album-art-in-ogg-through-command-line-in-linux
// * ffmpeg -i in.mp3 -i in.jpg -map 0 -map 1 -c copy -metadata:s:v title="Album cover" -metadata:s:v comment="Cover (Front)" out.mp3
// TODO: Allow for fetching lyrics?
// TODO: GUI for manual tag editing?
// TODO: Duplicate audio detection? This might be overkill.
// TODO: Discogs support?

package main

import (
	"bitbucket.org/ambrevar/demlo/cuesheet"
	"bytes"
	"crypto/md5"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/aarzilli/golua/lua"
	"github.com/mgutz/ansi"
	"github.com/wtolson/go-taglib"
	"github.com/yookoala/realpath"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

const (
	APPLICATION = "demlo"
	VERSION     = "2.0"
	COPYRIGHT   = "Copyright (C) 2013-2016 Pierre Neidhardt"
	URL         = "http://ambrevar.bitbucket.org/demlo"

	BLOCKSIZE            = 4096
	COVER_CHECKSUM_BLOCK = 8 * 4096
	CUESHEET_MAXSIZE     = 10 * 1024 * 1024
	INDEX_MAXSIZE        = 10 * 1024 * 1024
	SCRIPT_MAXSIZE       = 10 * 1024 * 1024
)

var usage = `Batch-transcode files with user-written scripts for dynamic tagging
and encoding.

Folders are processed recursively. Only files with known extensions are processed.
New extensions can be specified from command-line.

All flags that do not require an argument are booleans. Without argument, they
take the true value. To negate them, use the form '-flag=false'.

See ` + URL + ` for more details.
`

var (
	XDG_CONFIG_HOME = os.Getenv("XDG_CONFIG_HOME")
	XDG_CONFIG_DIRS = os.Getenv("XDG_CONFIG_DIRS")
	XDG_DATA_DIRS   = os.Getenv("XDG_DATA_DIRS")

	SYSTEM_SCRIPTROOT string
	USER_SCRIPTROOT   string
	CONFIG            string

	COLOR_OLD      = ""
	COLOR_NEW      = ""
	COVER_EXT_LIST = map[string]bool{"gif": true, "jpeg": true, "jpg": true, "png": true}

	OPTIONS = Options{}

	CACHE = struct {
		index   map[string][]outputDesc
		scripts []scriptBuffer
	}{}

	DST_COVER_CACHE = map[dstCoverKey]bool{}

	DST_COVER_CACHE_MUTEX chan bool
)

type dstCoverKey struct {
	path     string
	checksum string
}

// Options used in the config file and/or as CLI flags.
// Precedence: flags > config > defaults.
// Exception: extensions specified in flags are merged with config extensions.
type Options struct {
	color        bool
	cores        int
	debug        bool
	extensions   stringSetFlag
	getcover     bool
	gettags      bool
	graphical    bool
	index        string
	overwrite    bool
	postscript   string
	prescript    string
	process      bool
	removesource bool
	scripts      scriptSlice
}

type scriptBuffer struct {
	name string
	buf  string
}

type scriptSlice []string

func (s *scriptSlice) String() string {
	// Print the default/config value.
	return fmt.Sprintf("%q", OPTIONS.scripts)
}
func (s *scriptSlice) Set(arg string) error {
	*s = append(*s, arg)
	return nil
}

type stringSetFlag map[string]bool

func (s *stringSetFlag) String() string {
	keylist := []string{}
	for k := range *s {
		keylist = append(keylist, k)
	}
	sort.Strings(keylist)
	return ": " + strings.Join(keylist, " ")
}
func (s *stringSetFlag) Set(arg string) error {
	(*s)[arg] = true
	return nil
}

type inputCover struct {
	// Supported format: gif, jpeg, png.
	format string

	// Size.
	width  int
	height int

	// Cover checksum is partial. This speeds up the process but can yield false duplicates.
	checksum string
}

type outputCover struct {
	Path       string
	Format     string
	Parameters []string
}

// TODO: Export all fields? Probably not a good idea: if FFprobe output changes,
// it could lead to undesired field overwriting.
// TODO: We cannot create an 'input struct' if we want all entries. However we
// can use a struct to unmarshal easily to known types. So we can use 2
// unmarshals: one to a struct for processing, one to an interface{} to pass to
// Lua.
type inputDesc struct {
	path    string // Realpath.
	bitrate int    // In bytes per second.
	tags    map[string]string

	embeddedCovers []inputCover
	externalCovers map[string]inputCover
	onlineCover    inputCover

	// Index of the first audio stream.
	audioIndex int

	// FFmpeg data.
	Streams []struct {
		Bit_rate   string
		Codec_name string
		Codec_type string
		Duration   string
		Height     int
		Tags       map[string]string
		Width      int
	}
	Format struct {
		Bit_rate    string
		Duration    string
		Format_name string
		Nb_streams  int
		Tags        map[string]string
	}

	// The following details for multi-track files are not transferred to Lua.
	filetags map[string]string
	cuesheet cuesheet.Cuesheet
	// Name of the matching file in the cuesheet.
	cuesheetFile string
	trackCount   int
}

// We could store everything in 'parameters', but having a separate 'path' and
// 'format' allows for foolproofing.
type outputDesc struct {
	Path           string
	Format         string
	Parameters     []string
	Tags           map[string]string
	EmbeddedCovers []outputCover
	ExternalCovers map[string]outputCover
	OnlineCover    outputCover
}

////////////////////////////////////////////////////////////////////////////////

// The format is:
//   [input] | attr | [output]
func prettyPrint(attr, input, output string, attrMaxlen, valueMaxlen int, display *Slogger) {
	colorIn := ""
	colorOut := ""
	if OPTIONS.color && input != output &&
		(attr != "parameters" || output != "[-c:a copy]") &&
		((attr != "embedded" && attr != "external") || (len(output) >= 3 && output[len(output)-3:] != " ''")) {
		colorIn = "red"
		colorOut = "green"
	}

	in := []rune(input)
	out := []rune(output)

	min := func(a, b int) int {
		if a < b {
			return a
		}
		return b
	}

	// Print first line with title.
	display.Output.Printf(
		"%*v["+ansi.Color("%.*s", colorIn)+"] | %-*v | ["+ansi.Color("%.*s", colorOut)+"]\n",
		valueMaxlen-min(valueMaxlen, len(in)), "",
		valueMaxlen, input,
		attrMaxlen, attr,
		valueMaxlen, output)

	// Print the rest that does not fit on first line.
	for i := valueMaxlen; i < len(in) || i < len(out); i += valueMaxlen {
		in_lo := min(i, len(in))
		in_hi := min(i+valueMaxlen, len(in))
		out_lo := min(i, len(out))
		out_hi := min(i+valueMaxlen, len(out))

		in_delim_left, in_delim_right := "[", "]"
		out_delim_left, out_delim_right := "[", "]"
		if i >= len(in) {
			in_delim_left, in_delim_right = " ", " "
		}
		if i >= len(out) {
			out_delim_left, out_delim_right = "", ""
		}

		display.Output.Printf(
			"%s"+ansi.Color("%s", colorIn)+"%s%*v | %*v | %s"+ansi.Color("%s", colorOut)+"%s\n",
			in_delim_left,
			string(in[in_lo:in_hi]),
			in_delim_right,
			valueMaxlen-in_hi+in_lo, "",
			attrMaxlen, "",
			out_delim_left,
			string(out[out_lo:out_hi]),
			out_delim_right)
	}
}

func preview(input inputDesc, output outputDesc, track int, display *Slogger) {
	prepareTrackTags(input, track)

	attrMaxlen := len("parameters")

	for k := range input.tags {
		if len(k) > attrMaxlen {
			attrMaxlen = len(k)
		}
	}
	for k := range output.Tags {
		if len(k) > attrMaxlen {
			attrMaxlen = len(k)
		}
	}

	maxCols, _, err := TerminalSize(int(os.Stdout.Fd()))
	if err != nil {
		log.Fatal(err)
	}
	// 'valueMaxlen' is the available width for input and output values. We
	// subtract some characters for the ' | ' around the attribute name and the
	// brackets around the values.
	valueMaxlen := (maxCols - attrMaxlen - 10) / 2

	// Sort tags.
	var tagList []string
	for k := range input.tags {
		tagList = append(tagList, k)
	}
	for k := range output.Tags {
		_, ok := input.tags[k]
		if !ok {
			tagList = append(tagList, k)
		}
	}
	sort.Strings(tagList)

	colorTitle := ""
	if OPTIONS.color {
		colorTitle = "white+b"
	}

	display.Output.Println()

	display.Output.Printf("%*v === "+ansi.Color("%-*v", colorTitle)+" ===\n",
		valueMaxlen, "",
		attrMaxlen, "FILE")
	prettyPrint("path", input.path, output.Path, attrMaxlen, valueMaxlen, display)
	prettyPrint("format", input.Format.Format_name, output.Format, attrMaxlen, valueMaxlen, display)
	prettyPrint("parameters", "bitrate="+strconv.Itoa(input.bitrate), fmt.Sprintf("%v", output.Parameters), attrMaxlen, valueMaxlen, display)

	display.Output.Printf("%*v === "+ansi.Color("%-*v", colorTitle)+" ===\n",
		valueMaxlen, "",
		attrMaxlen, "TAGS")
	for _, v := range tagList {
		// "encoder" is a field that is usually out of control, discard it.
		if v != "encoder" {
			prettyPrint(v, input.tags[v], output.Tags[v], attrMaxlen, valueMaxlen, display)
		}
	}

	display.Output.Printf("%*v === "+ansi.Color("%-*v", colorTitle)+" ===\n",
		valueMaxlen, "",
		attrMaxlen, "COVERS")
	for stream, cover := range input.embeddedCovers {
		in := fmt.Sprintf("'stream %v' [%vx%v] <%v>", stream, cover.width, cover.height, cover.format)
		out := "<> [] ''"
		if stream < len(output.EmbeddedCovers) {
			out = fmt.Sprintf("<%v> %q '%v'", output.EmbeddedCovers[stream].Format, output.EmbeddedCovers[stream].Parameters, output.EmbeddedCovers[stream].Path)
		}
		prettyPrint("embedded", in, out, attrMaxlen, valueMaxlen, display)
	}
	for file, cover := range input.externalCovers {
		in := fmt.Sprintf("'%v' [%vx%v] <%v>", file, cover.width, cover.height, cover.format)
		out := fmt.Sprintf("<%v> %q '%v'", output.ExternalCovers[file].Format, output.ExternalCovers[file].Parameters, output.ExternalCovers[file].Path)
		prettyPrint("external", in, out, attrMaxlen, valueMaxlen, display)
	}
	if input.onlineCover.format != "" {
		cover := input.onlineCover
		in := fmt.Sprintf("[%vx%v] <%v>", cover.width, cover.height, cover.format)
		out := fmt.Sprintf("<%v> %q '%v'", output.OnlineCover.Format, output.OnlineCover.Parameters, output.OnlineCover.Path)
		prettyPrint("online", in, out, attrMaxlen, valueMaxlen, display)
	}

	display.Output.Println()
}

func getEmbeddedCover(input inputDesc, display *Slogger) (embeddedCovers []inputCover, embeddedCoversCache [][]byte) {
	// FFmpeg treats embedded covers like video streams.
	for i := 0; i < input.Format.Nb_streams; i++ {
		if input.Streams[i].Codec_name != "image2" &&
			input.Streams[i].Codec_name != "mjpeg" {
			continue
		}

		cmd := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-y", "-i", input.path, "-an", "-sn", "-c:v", "copy", "-f", "image2", "-map", "0:"+strconv.Itoa(i), "-")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		cover, err := cmd.Output()
		if err != nil {
			display.Error.Printf(stderr.String())
			continue
		}

		reader := bytes.NewBuffer(cover)
		config, format, err := image.DecodeConfig(reader)
		if err != nil {
			display.Warning.Print(err)
			continue
		}

		hi := len(cover)
		if hi > COVER_CHECKSUM_BLOCK {
			hi = COVER_CHECKSUM_BLOCK
		}
		checksum := fmt.Sprintf("%x", md5.Sum(cover[:hi]))

		embeddedCoversCache = append(embeddedCoversCache, cover)
		embeddedCovers = append(embeddedCovers, inputCover{format: format, width: config.Width, height: config.Height, checksum: checksum})
	}

	return embeddedCovers, embeddedCoversCache
}

func getExternalCover(input inputDesc, display *Slogger) (externalCovers map[string]inputCover, err error) {
	// TODO: Memoize external cover queries.
	fd, err := os.Open(filepath.Dir(input.path))
	if err != nil {
		return nil, err
	}
	names, err := fd.Readdirnames(-1)
	fd.Close()
	if err != nil {
		return nil, err
	}

	externalCovers = make(map[string]inputCover)

	for _, f := range names {
		if !COVER_EXT_LIST[Ext(f)] {
			continue
		}
		fd, err := os.Open(filepath.Join(filepath.Dir(input.path), f))
		if err != nil {
			display.Warning.Print(err)
			continue
		}
		defer fd.Close()

		st, err := fd.Stat()
		if err != nil {
			display.Warning.Print(err)
			continue
		}

		config, format, err := image.DecodeConfig(fd)
		if err != nil {
			display.Warning.Print(err)
			continue
		}

		hi := st.Size()
		if hi > COVER_CHECKSUM_BLOCK {
			hi = COVER_CHECKSUM_BLOCK
		}

		buf := [COVER_CHECKSUM_BLOCK]byte{}
		_, err = (*fd).ReadAt(buf[:hi], 0)
		if err != nil && err != io.EOF {
			display.Warning.Print(err)
			continue
		}
		checksum := fmt.Sprintf("%x", md5.Sum(buf[:hi]))

		externalCovers[f] = inputCover{format: format, width: config.Width, height: config.Height, checksum: checksum}
	}

	return externalCovers, nil
}

func prepareTags(input *inputDesc, display *Slogger) {
	input.tags = make(map[string]string)
	input.filetags = make(map[string]string)

	// Precedence: cuesheet > stream tags > format tags.
	for k, v := range input.Format.Tags {
		input.filetags[strings.ToLower(k)] = v
	}
	for k, v := range input.Streams[input.audioIndex].Tags {
		key := strings.ToLower(k)
		_, ok := input.filetags[key]
		if !ok || input.filetags[key] == "" {
			input.filetags[key] = v
		}
	}

	var err error
	input.cuesheet, err = cuesheet.New(input.filetags["cuesheet"])
	if err != nil {
		// If no cuesheet was found in the tags, we check for external ones.
		pathNoext := StripExt(input.path)
		// Instead of checking the extension of files in current folder, we check
		// if a file with the 'cue' extension exists. This is faster, especially
		// for huge folders.
		for _, ext := range []string{"cue", "cuE", "cUe", "cUE", "Cue", "CuE", "CUe", "CUE"} {
			cs := pathNoext + "." + ext
			st, err := os.Stat(cs)
			if err != nil {
				continue
			}
			if st.Size() > CUESHEET_MAXSIZE {
				display.Warning.Print("Cuesheet size %v > %v bytes, skipping", cs, CUESHEET_MAXSIZE)
				continue
			}
			buf, err := ioutil.ReadFile(cs)
			if err != nil {
				display.Warning.Print(err)
				continue
			}
			input.cuesheet, err = cuesheet.New(string(buf))
			break
		}
	}
	// Remove cuesheet from tags to avoid printing it.
	delete(input.filetags, "cuesheet")

	// The number of tracks in current file is usually 1, it can be more if a
	// cuesheet is found.
	input.trackCount = 1
	if input.cuesheet.Files != nil {
		// Copy the cuesheet header to the tags. Some entries appear both in the
		// header and in the track details. We map the cuesheet header entries to
		// the respective quivalent for FFmpeg tags.
		for k, v := range input.cuesheet.Header {
			switch k {
			case "PERFORMER":
				input.filetags["album_artist"] = v
			case "SONGWRITER":
				input.filetags["album_artist"] = v
			case "TITLE":
				input.filetags["album"] = v
			default:
				input.filetags[strings.ToLower(k)] = v
			}
		}

		// A cuesheet might have several FILE entries, or even none (non-standard).
		// In case of none, tracks are stored at file "" (the empty string) in the
		// Cuesheet structure. Otherwise, we find the most related file.
		base := stringNorm(filepath.Base(input.path))
		max := 0.0
		for f := range input.cuesheet.Files {
			r := stringRel(stringNorm(f), base)
			if r > max {
				max = r
				input.cuesheetFile = f
			}
		}
		input.trackCount = len(input.cuesheet.Files[input.cuesheetFile])
	}
}

func prepareTrackTags(input inputDesc, track int) {
	// Copy all tags from input.filetags to input.tags.
	for k, v := range input.filetags {
		input.tags[k] = v
	}

	if len(input.cuesheet.Files) > 0 {
		// If there is a cuesheet, we fetch track tags as required. Note that this
		// process differs from the above cuesheet extraction in that it is
		// track-related as opposed to album-related. Cuesheets make a distinction
		// between the two. Some tags may appear both in an album field and a track
		// field. Thus track tags must have higher priority.
		for k, v := range input.cuesheet.Files[input.cuesheetFile][track].Tags {
			input.tags[strings.ToLower(k)] = v
		}
	}
}

func runAllScripts(input inputDesc, track int, defaultTags map[string]string, L *lua.State, display *Slogger) (output outputDesc) {
	prepareTrackTags(input, track)

	if o, ok := CACHE.index[input.path]; ok && len(o) > track {
		output = CACHE.index[input.path][track]
		OPTIONS.gettags = false
	} else {

		// Default tags.
		output.Tags = make(map[string]string)
		for k, v := range input.tags {
			output.Tags[k] = v
		}
		for k, v := range defaultTags {
			output.Tags[k] = v
		}

		// Default codec options.
		output.Format = input.Format.Format_name
	}

	// Create a Lua sandbox containing input and output, then run scripts.
	makeSandboxOutput(L, output)
	for _, script := range CACHE.scripts {
		err := runScript(L, script.name, input)
		if err != nil {
			display.Error.Printf("Script %s: %s", script.name, err)
			continue
		}
	}
	output = scriptOutput(L)

	// Foolproofing.
	// -No format: use input.format.
	// -No parameters: use "-c:a copy".
	// -Empty output basename: use input path.
	// -Remove empty tags to avoid storing empty strings in FFmpeg.

	if output.Format == "" {
		output.Format = input.Format.Format_name
	}

	if len(output.Parameters) == 0 {
		output.Parameters = []string{"-c:a", "copy"}
	}

	if Basename(output.Path) == "" {
		output.Path = input.path
	}

	var err error
	output.Path, err = filepath.Abs(output.Path)
	if err != nil {
		display.Warning.Print("Cannot get absolute path:", err)
		return output
	}

	for tag, value := range output.Tags {
		if value == "" {
			delete(output.Tags, tag)
		}
	}

	return output
}

// Create a new destination file 'dst'.
// As a special case, if 'inputPath == dst' and 'removesource == true',
// then modify the file inplace.
// If no third-party program overwrites existing files, this approach cannot
// clobber existing files.
func makeTrackDst(dst string, inputPath string, removeSource bool) (string, error) {
	if _, err := os.Stat(dst); err == nil || !os.IsNotExist(err) {
		// 'dst' exists.
		// The realpath is required to check if inplace.
		// The 'realpath' can only be expanded when the parent folder exists.
		dst, err = realpath.Realpath(dst)
		if err != nil {
			return "", err
		}

		if inputPath != dst || !removeSource {
			// If not inplace, create a temp file.
			f, err := TempFile(filepath.Dir(dst), StripExt(filepath.Base(dst))+"_", "."+Ext(dst))
			if err != nil {
				return "", err
			}
			dst = f.Name()
			f.Close()
		}
	} else {
		st, err := os.Stat(inputPath)
		if err != nil {
			return "", err
		}

		f, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL, st.Mode())
		if err != nil {
			// Either the parent folder is not writable, or a race condition happened:
			// file was created between existence check and file creation.
			return "", err
		}
		f.Close()
	}

	return dst, nil
}

// Create a new destination file 'dst'. See makeTrackDst.
// As a special case, if the checksums match in input and dst, return "", nil.
// TODO: Test how memoization scales with DST_COVER_CACHE.
func makeCoverDst(dst string, inputPath string, checksum string, display *Slogger) (string, error) {
	if st, err := os.Stat(dst); err == nil || !os.IsNotExist(err) {
		// 'dst' exists.

		// Realpath is required for cache key uniqueness.
		dst, err = realpath.Realpath(dst)
		if err != nil {
			return "", err
		}

		// Skip if marked in cache.
		<-DST_COVER_CACHE_MUTEX
		marked := DST_COVER_CACHE[dstCoverKey{path: dst, checksum: checksum}]
		DST_COVER_CACHE_MUTEX <- true
		if marked {
			return "", nil
		} else {
			<-DST_COVER_CACHE_MUTEX
			DST_COVER_CACHE[dstCoverKey{path: dst, checksum: checksum}] = true
			DST_COVER_CACHE_MUTEX <- true
		}

		// Compute checksum of existing cover and early-out if equal.
		fd, err := os.Open(dst)
		if err != nil {
			return "", err
		}
		defer fd.Close()

		// TODO: Cache checksums.
		hi := st.Size()
		if hi > COVER_CHECKSUM_BLOCK {
			hi = COVER_CHECKSUM_BLOCK
		}

		buf := [COVER_CHECKSUM_BLOCK]byte{}
		_, err = (*fd).ReadAt(buf[:hi], 0)
		if err != nil && err != io.EOF {
			return "", err
		}
		dstChecksum := fmt.Sprintf("%x", md5.Sum(buf[:hi]))

		if checksum == dstChecksum {
			return "", nil
		}

		// If not inplace, create a temp file.
		f, err := TempFile(filepath.Dir(dst), StripExt(filepath.Base(dst))+"_", "."+Ext(dst))
		if err != nil {
			return "", err
		}
		dst = f.Name()
		f.Close()
	} else {
		st, err := os.Stat(inputPath)
		if err != nil {
			return "", err
		}

		f, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL, st.Mode())
		if err != nil {
			// Either the parent folder is not writable, or a race condition happened:
			// file was created between existence check and file creation.
			return "", err
		}
		f.Close()

		// Save to cache.
		dst, err = realpath.Realpath(dst)
		if err != nil {
			return "", err
		}
		<-DST_COVER_CACHE_MUTEX
		DST_COVER_CACHE[dstCoverKey{path: dst, checksum: checksum}] = true
		DST_COVER_CACHE_MUTEX <- true
	}

	return dst, nil
}

func transferCovers(cover outputCover, coverName string, inputPath string, inputSource io.Reader, checksum string, display *Slogger) {
	var err error
	if cover.Path == "" {
		return
	}
	if len(cover.Parameters) == 0 || cover.Format == "" {
		cover.Path, err = makeCoverDst(cover.Path, inputPath, checksum, display)
		if err != nil {
			display.Error.Print(err)
			return
		}
		if cover.Path == "" {
			// Identical file exists.
			return
		}

		dstFd, err := os.OpenFile(cover.Path, os.O_WRONLY|os.O_TRUNC, 0666)
		if err != nil {
			display.Warning.Println(err)
			return
		}

		if _, err = io.Copy(dstFd, inputSource); err != nil {
			display.Warning.Println(err)
			return
		}
		dstFd.Close()

	} else {
		cover.Path, err = makeCoverDst(cover.Path, inputPath, checksum, display)
		if err != nil {
			display.Error.Print(err)
			return
		}
		if cover.Path == "" {
			// Identical file exists.
			return
		}

		cmdArray := []string{"-nostdin", "-v", "error", "-y", "-i", "-", "-an", "-sn"}
		cmdArray = append(cmdArray, cover.Parameters...)
		cmdArray = append(cmdArray, "-f", cover.Format, cover.Path)

		display.Debug.Printf("Cover %v parameters: %q", coverName, cmdArray)

		cmd := exec.Command("ffmpeg", cmdArray...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		cmd.Stdin = inputSource

		_, err := cmd.Output()
		if err != nil {
			display.Warning.Printf(stderr.String())
			return
		}
	}
}

// goroutine main function, a.k.a worker.
// 'queue' contains realpaths to files.
func process(queue chan string, quit chan bool) {
	defer func() { quit <- true }()
	display := newSlogger(OPTIONS.debug, OPTIONS.color)
	defer display.Flush()

	// Compile scripts.
	L, err := makeSandbox(CACHE.scripts, display)
	if err != nil {
		display.Error.Print(err)
	}
	defer L.Close()

	for file := range queue {
		display.Flush()
		display.Section.Println(file)

		cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", "-show_format", file)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		out, err := cmd.Output()
		if err != nil {
			display.Error.Print("ffprobe: ", stderr.String())
			continue
		}

		var input inputDesc

		err = json.Unmarshal(out, &input)
		if err != nil {
			display.Error.Print(err)
			continue
		}

		input.path = file // realpath

		// Index of the first audio stream.
		input.audioIndex = -1
		for k, v := range input.Streams {
			if v.Codec_type == "audio" {
				input.audioIndex = k
				break
			}
		}
		if input.audioIndex == -1 {
			display.Warning.Print("Non-audio file:", input.path)
			continue
		}

		// Set bitrate.
		// FFmpeg stores bitrate as a string, Demlo needs a number. If
		// 'streams[audioIndex].bit_rate' is empty (e.g. in APE files), look for
		// 'format.bit_rate'. To ease querying bitrate from user scripts, store it
		// in 'input.bitrate'.
		input.bitrate, err = strconv.Atoi(input.Streams[input.audioIndex].Bit_rate)
		if err != nil {
			input.bitrate, err = strconv.Atoi(input.Format.Bit_rate)
			if err != nil {
				display.Warning.Print("Cannot get bitrate from", input.path)
				continue
			}
		}

		// prepareTags should be run before setting the covers.
		prepareTags(&input, display)

		input.externalCovers, err = getExternalCover(input, display)
		if err != nil {
			display.Warning.Print(err)
			continue
		}

		var embeddedCoversCache [][]byte
		var onlineCoverCache []byte
		input.embeddedCovers, embeddedCoversCache = getEmbeddedCover(input, display)
		var defaultTags map[string]string

		// We retrieve tags online only for single-track files. TODO: Add support for multi-track files.
		if input.trackCount == 1 {
			var releaseID ReleaseID
			prepareTrackTags(input, 1)
			if OPTIONS.gettags {
				releaseID, defaultTags, err = getOnlineTags(input, display)
				if err != nil {
					display.Debug.Print("Online tags query error: ", err)
				}
			}
			if OPTIONS.getcover {
				onlineCoverCache, input.onlineCover, err = getOnlineCover(input, releaseID, display)
				if err != nil {
					display.Debug.Print("Online cover query error: ", err)
				}
			}
		}

		var output = make([]outputDesc, input.trackCount)
		for track := 0; track < input.trackCount; track++ {
			output[track] = runAllScripts(input, track, defaultTags, L, display)
		}

		//--------------------------------------------------------------------------------
		// Preview.

		if OPTIONS.graphical {
			for track := 0; track < input.trackCount; track++ {
				preview(input, output[track], track, display)
				// Warn for existence.
				_, err = os.Stat(output[track].Path)
				if err == nil || !os.IsNotExist(err) {
					display.Warning.Println("Destination exists:", output[track].Path)
				}
			}
		} else {
			// Should never fail.
			buf1, _ := json.Marshal(input.path)
			buf2, _ := json.MarshalIndent(output, "", "\t")
			display.Output.Printf("%s: %s,\n", buf1, buf2)
		}

		if !OPTIONS.process {
			continue
		}

		//--------------------------------------------------------------------------------
		// Re-encode / copy / rename.
		for track := 0; track < input.trackCount; track++ {
			err = os.MkdirAll(filepath.Dir(output[track].Path), 0777)
			if err != nil {
				display.Error.Print(err)
				continue
			}

			// Copy embeddedCovers, externalCovers and onlineCover.
			for stream, cover := range output[track].EmbeddedCovers {
				inputSource := bytes.NewBuffer(embeddedCoversCache[stream])
				transferCovers(cover, "embedded "+strconv.Itoa(stream), input.path, inputSource, input.embeddedCovers[stream].checksum, display)
			}
			for file, cover := range output[track].ExternalCovers {
				inputPath := filepath.Join(filepath.Dir(input.path), file)
				inputSource, err := os.Open(inputPath)
				if err != nil {
					continue
				}
				transferCovers(cover, "external '"+file+"'", inputPath, inputSource, input.externalCovers[file].checksum, display)
				inputSource.Close()
			}
			{
				inputSource := bytes.NewBuffer(onlineCoverCache)
				transferCovers(output[track].OnlineCover, "online", input.path, inputSource, input.onlineCover.checksum, display)
			}

			// If encoding changed, use FFmpeg. Otherwise, copy/rename the file to
			// speed up the process. If tags have changed but not the encoding, we use
			// taglib to set them.
			var encodingChanged = false
			var tagsChanged = false

			if input.trackCount > 1 {
				// Split cue-sheet.
				encodingChanged = true
			}

			if input.Format.Format_name != output[track].Format {
				encodingChanged = true
			}

			if len(output[track].Parameters) != 2 ||
				output[track].Parameters[0] != "-c:a" ||
				output[track].Parameters[1] != "copy" {
				encodingChanged = true
			}

			// Test if tags have changed.
			for k, v := range input.tags {
				if k != "encoder" && output[track].Tags[k] != v {
					tagsChanged = true
					break
				}
			}
			if !tagsChanged {
				for k, v := range output[track].Tags {
					if k != "encoder" && input.tags[k] != v {
						tagsChanged = true
						break
					}
				}
			}

			// TODO: Move this to 2/3 separate functions.
			// TODO: Add to condition: `|| output[track].format == "taglib-unsupported-format"`.
			if encodingChanged {
				// Store encoding parameters.
				ffmpegParameters := []string{}

				// Be verbose only when running a single process. Otherwise output gets
				// would get messy.
				if OPTIONS.cores > 1 {
					ffmpegParameters = append(ffmpegParameters, "-v", "warning")
				} else {
					ffmpegParameters = append(ffmpegParameters, "-v", "error")
				}

				// By default, FFmpeg reads stdin while running. Disable this feature to
				// avoid unexpected problems.
				ffmpegParameters = append(ffmpegParameters, "-nostdin")

				// FFmpeg should always overwrite: if a temp file is created to avoid
				// overwriting, FFmpeg should clobber it.
				ffmpegParameters = append(ffmpegParameters, "-y")

				ffmpegParameters = append(ffmpegParameters, "-i", input.path)

				// Stream codec.
				ffmpegParameters = append(ffmpegParameters, output[track].Parameters...)

				// Get cuesheet splitting parameters.
				if len(input.cuesheet.Files) > 0 {
					d, _ := strconv.ParseFloat(input.Streams[input.audioIndex].Duration, 64)
					start, duration := FFmpegSplitTimes(input.cuesheet, input.cuesheetFile, track, d)
					ffmpegParameters = append(ffmpegParameters, "-ss", start, "-t", duration)
				}

				// If there are no covers, do not copy any video stream to avoid errors.
				if input.Format.Nb_streams < 2 {
					ffmpegParameters = append(ffmpegParameters, "-vn")
				}

				// Remove non-cover streams and extra audio streams.
				// Must add all streams first.
				ffmpegParameters = append(ffmpegParameters, "-map", "0")
				for i := 0; i < input.Format.Nb_streams; i++ {
					if (input.Streams[i].Codec_type == "video" && input.Streams[i].Codec_name != "image2" && input.Streams[i].Codec_name != "mjpeg") ||
						(input.Streams[i].Codec_type == "audio" && i > input.audioIndex) ||
						(input.Streams[i].Codec_type != "audio" && input.Streams[i].Codec_type != "video") {
						ffmpegParameters = append(ffmpegParameters, "-map", "-0:"+strconv.Itoa(i))
					}
				}

				// Remove subtitles if any.
				ffmpegParameters = append(ffmpegParameters, "-sn")

				// '-map_metadata -1' clears all metadata first.
				ffmpegParameters = append(ffmpegParameters, "-map_metadata", "-1")

				for tag, value := range output[track].Tags {
					ffmpegParameters = append(ffmpegParameters, "-metadata", tag+"="+value)
				}

				// Format.
				ffmpegParameters = append(ffmpegParameters, "-f", output[track].Format)

				// Output file.
				// FFmpeg cannot transcode inplace, so we force creating a temp file if
				// necessary.
				var dst string
				dst, err := makeTrackDst(output[track].Path, input.path, false)
				if err != nil {
					display.Error.Print(err)
					continue
				}
				ffmpegParameters = append(ffmpegParameters, dst)

				display.Debug.Printf("Audio %v parameters: %q", track, ffmpegParameters)

				cmd := exec.Command("ffmpeg", ffmpegParameters...)
				var stderr bytes.Buffer
				cmd.Stderr = &stderr

				err = cmd.Run()
				if err != nil {
					display.Error.Printf(stderr.String())
					continue
				}

				if OPTIONS.removesource {
					// TODO: This realpath is already expanded in 'makeTrackDst'. Factor
					// it.
					output[track].Path, err = realpath.Realpath(output[track].Path)
					if err != nil {
						display.Error.Print(err)
						continue
					}
					if input.path == output[track].Path {
						// If inplace, rename.
						err = os.Rename(dst, output[track].Path)
						if err != nil {
							display.Error.Print(err)
						}
					} else {
						err = os.Remove(input.path)
						if err != nil {
							display.Error.Print(err)
						}
					}
				}
			} else {
				var err error
				var dst string
				dst, err = makeTrackDst(output[track].Path, input.path, OPTIONS.removesource)
				if err != nil {
					display.Error.Print(err)
					continue
				}

				if input.path != dst {
					// Copy/rename file if not inplace.
					err = nil
					if OPTIONS.removesource {
						err = os.Rename(input.path, dst)
					}
					if err != nil || !OPTIONS.removesource {
						// If renaming failed, it might be because of a cross-device
						// destination. We try to copy instead.
						err := CopyFile(dst, input.path)
						if err != nil {
							display.Error.Println(err)
							continue
						}
						if OPTIONS.removesource {
							err = os.Remove(input.path)
							if err != nil {
								display.Error.Println(err)
							}
						}
					}
				}

				if tagsChanged {
					// TODO: Can TagLib remove extra tags?
					f, err := taglib.Read(dst)
					if err != nil {
						display.Error.Print(err)
						continue
					}
					defer f.Close()

					// TODO: Arbitrary tag support with taglib?
					if output[track].Tags["album"] != "" {
						f.SetAlbum(output[track].Tags["album"])
					}
					if output[track].Tags["artist"] != "" {
						f.SetArtist(output[track].Tags["artist"])
					}
					if output[track].Tags["comment"] != "" {
						f.SetComment(output[track].Tags["comment"])
					}
					if output[track].Tags["genre"] != "" {
						f.SetGenre(output[track].Tags["genre"])
					}
					if output[track].Tags["title"] != "" {
						f.SetTitle(output[track].Tags["title"])
					}
					if output[track].Tags["track"] != "" {
						t, err := strconv.Atoi(output[track].Tags["track"])
						if err == nil {
							f.SetTrack(t)
						}
					}
					if output[track].Tags["date"] != "" {
						t, err := strconv.Atoi(output[track].Tags["date"])
						if err == nil {
							f.SetYear(t)
						}
					}

					err = f.Save()
					if err != nil {
						display.Error.Print(err)
					}
				}
			}
		}
	}
}

// Return the first existing match from 'list'.
func findscript(name string) (path string, st os.FileInfo, err error) {
	name_ext := name + ".lua"
	list := []string{
		name,
		name_ext,
		filepath.Join(USER_SCRIPTROOT, name),
		filepath.Join(USER_SCRIPTROOT, name_ext),
		filepath.Join(SYSTEM_SCRIPTROOT, name),
		filepath.Join(SYSTEM_SCRIPTROOT, name_ext),
	}
	for _, path := range list {
		if st, err := os.Stat(path); err == nil {
			return path, st, nil
		}
	}
	return "", nil, errors.New("Script not found")
}

// Note to packagers: those following lines can be patched to fit the local
// filesystem.
func init() {
	log.SetFlags(0)

	if XDG_CONFIG_HOME == "" {
		XDG_CONFIG_HOME = filepath.Join(os.Getenv("HOME"), ".config")
	}

	if XDG_CONFIG_DIRS == "" {
		XDG_CONFIG_DIRS = "/etc/xdg"
	}

	if XDG_DATA_DIRS == "" {
		XDG_DATA_DIRS = "/usr/local/share/:/usr/share"
	}

	pathlistSub := func(pathlist, subpath string) string {
		for _, dir := range filepath.SplitList(pathlist) {
			if dir == "" {
				dir = "."
			}
			file := filepath.Join(dir, subpath)
			_, err := os.Stat(file)
			if err == nil {
				return file
			}
		}
		return ""
	}

	SYSTEM_SCRIPTROOT = pathlistSub(XDG_DATA_DIRS, filepath.Join(APPLICATION, "scripts"))
	USER_SCRIPTROOT = pathlistSub(XDG_CONFIG_HOME, filepath.Join(APPLICATION, "scripts"))

	CONFIG = os.Getenv("DEMLORC")
	if CONFIG == "" {
		CONFIG = filepath.Join(XDG_CONFIG_HOME, APPLICATION, APPLICATION+"rc")
	}

	DST_COVER_CACHE_MUTEX = make(chan bool, 1)
	DST_COVER_CACHE_MUTEX <- true
}

func main() {
	// Load config first since it changes the default flag values.
	st, err := os.Stat(CONFIG)
	if err == nil && st.Mode().IsRegular() {
		fmt.Fprintf(os.Stderr, ":: Load config: %v\n", CONFIG)
		OPTIONS = loadConfig(CONFIG)
	}
	if OPTIONS.extensions == nil {
		// Defaults: Init here so that unspecified config options get properly set.
		OPTIONS.extensions = stringSetFlag{
			"aac":  true,
			"ape":  true,
			"flac": true,
			"ogg":  true,
			"m4a":  true,
			"mp3":  true,
			"mp4":  true,
			"mpc":  true,
			"wav":  true,
			"wv":   true,
		}
	}

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %v [OPTIONS] FILES|FOLDERS\n\n", os.Args[0])
		fmt.Fprintln(os.Stderr, usage)
		fmt.Fprintln(os.Stderr, "Options:")
		flag.PrintDefaults()
	}

	flag.BoolVar(&OPTIONS.color, "color", OPTIONS.color, "Color output.")
	flag.IntVar(&OPTIONS.cores, "cores", OPTIONS.cores, "Run N processes in parallel. If 0, use all online cores.")
	flag.BoolVar(&OPTIONS.debug, "debug", false, "Enable debug messages.")
	flag.Var(&OPTIONS.extensions, "ext", "Additional extensions to look for when a folder is browsed.")
	flag.BoolVar(&OPTIONS.getcover, "c", OPTIONS.getcover, "Fetch cover from the Internet.")
	flag.BoolVar(&OPTIONS.gettags, "t", OPTIONS.gettags, "Fetch tags from the Internet.")
	flag.BoolVar(&OPTIONS.graphical, "g", OPTIONS.graphical, "Use formatted output.")
	flag.StringVar(&OPTIONS.index, "i", OPTIONS.index, `Use index file to set input and output metadata.
    	The index can be built using the non-formatted preview output.`)
	flag.StringVar(&OPTIONS.postscript, "post", OPTIONS.postscript, "Run Lua commands after the other scripts.")
	flag.StringVar(&OPTIONS.prescript, "pre", OPTIONS.prescript, "Run Lua commands before the other scripts.")
	flag.BoolVar(&OPTIONS.process, "p", OPTIONS.process, "Apply changes: set tags and format, move/copy result to destination file.")
	flag.BoolVar(&OPTIONS.removesource, "rmsrc", OPTIONS.removesource, "Remove source file after processing.")
	var flagScripts scriptSlice
	flag.Var(&flagScripts, "s", `Specify scripts to run in provided order.
    	This option can be specified several times. If only the basename without extension is given,
    	and if it is not found in current folder, the corresponding standard script will be used.`)

	var flagVersion = flag.Bool("v", false, "Print version and exit.")

	flag.Parse()

	if *flagVersion {
		fmt.Println(APPLICATION, VERSION, COPYRIGHT)
		return
	}

	if flag.Arg(0) == "" {
		flag.Usage()
		return
	}

	// Check for essential programs.
	_, err = exec.LookPath("ffmpeg")
	if err != nil {
		log.Fatal(err)
	}
	_, err = exec.LookPath("ffprobe")
	if err != nil {
		log.Fatal(err)
	}

	// Disable formatted output if piped.
	st, _ = os.Stdout.Stat()
	if (st.Mode() & os.ModeCharDevice) == 0 {
		OPTIONS.graphical = false
	}
	st, _ = os.Stderr.Stat()
	if (st.Mode() & os.ModeCharDevice) == 0 {
		OPTIONS.color = false
	}

	// Main logger.
	display := newSlogger(OPTIONS.debug, OPTIONS.color)

	// Load index to cache.
	if OPTIONS.index != "" {
		st, err := os.Stat(OPTIONS.index)
		if err != nil {
			display.Warning.Printf("Index not found: [%v]", OPTIONS.index)
		} else {
			if st.Size() > INDEX_MAXSIZE {
				display.Warning.Printf("Index size > %v bytes, skipping: %v", INDEX_MAXSIZE, OPTIONS.index)
			} else {
				buf, err := ioutil.ReadFile(OPTIONS.index)
				if err != nil {
					display.Warning.Print("Index is not readable:", err)
				} else {
					// Enclose JSON list in a valid structure. Since index ends with a
					// comma, hence the required dummy entry.
					buf = append(append([]byte{'{'}, buf...), []byte(`"": null}`)...)
					err = json.Unmarshal(buf, &CACHE.index)
					if err != nil {
						display.Warning.Printf("Invalid index %v: %v", OPTIONS.index, err)
					}
				}
			}
		}
	}

	// Load scripts to cache.
	if OPTIONS.prescript != "" {
		CACHE.scripts = append(CACHE.scripts, scriptBuffer{name: "prescript", buf: OPTIONS.prescript})
	}
	if len(flagScripts) > 0 {
		// CLI overrides default/config values.
		OPTIONS.scripts = flagScripts
	}
	for _, s := range OPTIONS.scripts {
		path, st, err := findscript(s)
		if err != nil {
			display.Warning.Printf("%v: %v", err, s)
			continue
		}
		if sz := st.Size(); sz > SCRIPT_MAXSIZE {
			display.Warning.Printf("Script size %v > %v bytes, skipping: %v", sz, SCRIPT_MAXSIZE, path)
			continue
		}
		buf, err := ioutil.ReadFile(path)
		if err != nil {
			display.Warning.Print("Script is not readable: ", err)
			continue
		}
		display.Info.Printf("Load script: %v", path)
		CACHE.scripts = append(CACHE.scripts, scriptBuffer{name: path, buf: string(buf)})
	}
	if OPTIONS.postscript != "" {
		CACHE.scripts = append(CACHE.scripts, scriptBuffer{name: "postscript", buf: OPTIONS.postscript})
	}

	// Limit number of cores to online cores.
	if OPTIONS.cores > runtime.NumCPU() || OPTIONS.cores <= 0 {
		OPTIONS.cores = runtime.NumCPU()
	}

	display.Flush()

	// If all workers are ready at the same time, they will query 'OPTIONS.cores'
	// files from the queue. Add some extra space to the queue in the unlikely
	// event the folder walk is slower than the workers.
	queue := make(chan string, 2*OPTIONS.cores)
	quit := make(chan bool, OPTIONS.cores)

	for i := 0; i < OPTIONS.cores; i++ {
		go process(queue, quit)
		// Wait for all routines.
		defer func() { <-quit }()
	}

	uniqueInput := map[string]bool{}
	for _, file := range flag.Args() {
		visit := func(path string, info os.FileInfo, err error) error {
			if err != nil || !info.Mode().IsRegular() {
				return nil
			}
			if !OPTIONS.extensions[strings.ToLower(Ext(path))] {
				return nil
			}
			rpath, err := realpath.Realpath(path)
			if err != nil {
				display.Error.Print("Cannot get real path:", err)
				display.Flush()
				return nil
			}
			if !uniqueInput[rpath] {
				uniqueInput[rpath] = true
				queue <- rpath
			}
			return nil
		}
		filepath.Walk(file, visit)
	}
	close(queue)
}
