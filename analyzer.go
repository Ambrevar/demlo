package main

import (
	"bytes"
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"bitbucket.org/ambrevar/demlo/cuesheet"
	"github.com/aarzilli/golua/lua"
	"github.com/mgutz/ansi"
)

var (
	coverExtList = map[string]bool{"gif": true, "jpeg": true, "jpg": true, "png": true}
	errNonAudio  = errors.New("non-audio file")
	rePrintable  = regexp.MustCompile(`\pC`)
	stdoutMutex  sync.Mutex
)

// analyzer loads file metadata into the file record, run the scripts and preview the result.
// If required, it will fetch additional input metadata online.
// This stage does not split elegantly:
// - defaultTags need to be passed to the running script.
// - The preview depends on prepareTrackTags.
type analyzer struct {
	L         *lua.State
	scriptLog *log.Logger
}

func (a *analyzer) Init() {
	// Script log output must be set for each FileRecord when calling the scripts.
	a.scriptLog = log.New(nil, "@@ ", 0)
	if options.Color {
		a.scriptLog.SetPrefix(ansi.Color(a.scriptLog.Prefix(), "cyan+b"))
	}

	// Compile scripts.
	var err error
	luaDebug := a.scriptLog.Println
	if !options.Debug {
		luaDebug = nil
	}
	a.L, err = MakeSandbox(luaDebug)
	if err != nil {
		log.Fatal(err)
	}

	for _, script := range cache.scripts {
		SandboxCompileScript(a.L, script.name, script.buf)
	}

	for name, action := range cache.actions {
		SandboxCompileAction(a.L, name, action)
	}
}

func (a *analyzer) Close() {
	a.L.Close()
}

func (a *analyzer) Run(fr *FileRecord) error {
	fr.section.Println(fr.input.path)

	// Should be run before setting the covers.
	err := prepareInput(fr, &fr.input)
	if err != nil {
		return err
	}

	// Shorthand.
	input := &fr.input

	err = getExternalCover(fr)
	if err != nil {
		fr.warning.Print(err)
		return err
	}

	getEmbeddedCover(fr)
	var defaultTags map[string]string

	// We retrieve tags online only for single-track files. TODO: Add support for multi-track files.
	if input.trackCount == 1 {
		var releaseID ReleaseID
		prepareTrackTags(input, 1)
		if options.Gettags {
			releaseID, defaultTags, err = GetOnlineTags(fr)
			if err != nil {
				fr.debug.Print("Online tags query error: ", err)
			}
		}
		if options.Getcover {
			fr.onlineCoverCache, input.onlineCover, err = GetOnlineCover(fr, releaseID)
			if err != nil {
				fr.debug.Print("Online cover query error: ", err)
			}
		}
	}

	fr.output = make([]outputInfo, input.trackCount)
	for track := 0; track < input.trackCount; track++ {
		err := a.RunAllScripts(fr, track, defaultTags)
		if err != nil {
			return err
		}
	}

	// Preview changes.
	if previewOptions.printDiff {
		for track := 0; track < input.trackCount; track++ {
			preview(fr, track)
		}
	}

	if previewOptions.printIndex {
		// Should never fail.
		buf1, _ := json.Marshal(input.path)
		buf2, _ := json.MarshalIndent(fr.output, "", "\t")
		stdoutMutex.Lock()
		fmt.Printf("%s: %s,\n", buf1, buf2)
		stdoutMutex.Unlock()
	}

	return nil
}

func (a *analyzer) RunAllScripts(fr *FileRecord, track int, defaultTags map[string]string) error {
	input := &fr.input
	output := &fr.output[track]

	prepareTrackTags(input, track)

	if o, ok := cache.index[input.path]; ok && len(o) > track {
		*output = cache.index[input.path][track]
		options.Gettags = false
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
		output.Format = fr.Format.FormatName
	}

	// Create a Lua sandbox containing input and output, then run scripts.
	a.scriptLog.SetOutput(&fr.logBuf)
	for _, script := range cache.scripts {
		err := RunScript(a.L, script.name, input, output)
		if err != nil {
			fr.error.Printf("Script %s: %s", script.name, err)
			return err
		}
	}

	// Foolproofing.
	// -No format: use input.format.
	// -No parameters: use "-c:a copy".
	// -Empty output basename: use input path.
	// -Remove empty tags to avoid storing empty strings in FFmpeg.

	if output.Format == "" {
		output.Format = fr.Format.FormatName
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
		fr.warning.Print("Cannot get absolute path:", err)
	}

	for tag, value := range output.Tags {
		if value == "" {
			delete(output.Tags, tag)
		}
	}

	// Check for existence.
	_, err = os.Stat(fr.output[track].Path)
	if err == nil || !os.IsNotExist(err) {
		if cache.actions[actionExist] != "" {
			fr.exist.path = fr.output[track].Path
			err := prepareInput(fr, &fr.exist)
			if err != nil {
				return err
			}
			prepareTrackTags(&fr.exist, track)
			err = RunAction(a.L, actionExist, input, output, &fr.exist)
			if err != nil {
				fr.error.Printf("Exist action: %s", err)
				return err
			}
		} else {
			// Don't always call above functions to save the analysis of existing
			// destination.
			fr.output[track].Write = "suffix"
		}

		switch fr.output[track].Write {
		case existWriteOver:
			fr.warning.Println("Overwrite existing destination:", fr.output[track].Path)
		case existWriteSkip:
			fr.warning.Println("Skip existing destination:", fr.output[track].Path)
		default:
			fr.output[track].Write = "suffix"
			fr.warning.Println("Append suffix to existing destination:", fr.output[track].Path)
		}
	} else {
		fr.output[track].Write = "nonexist"
	}

	return nil
}

// prepareInput sets the details of 'info' as returned by ffprobe.
// As a special case, if 'info' is 'fr.input', then 'fr.Format' and
// 'fr.Streams': those values will be needed later in the pipeline.
func prepareInput(fr *FileRecord, info *inputInfo) error {
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", "-show_format", info.path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		fr.error.Print("ffprobe: ", stderr.String())
		return err
	}

	err = json.Unmarshal(out, info)
	if err != nil {
		fr.error.Print(err)
		return err
	}

	// probed need not be initialized since we only use it to temporarily store
	// the 'Format' and 'Streams' structures returned by 'ffprobe'.
	var probed FileRecord
	err = json.Unmarshal(out, &probed)
	if err != nil {
		fr.error.Print(err)
		return err
	}

	if info == &fr.input {
		fr.Format = probed.Format
		fr.Streams = probed.Streams
	}

	// Index of the first audio stream.
	info.audioIndex = -1
	for k, v := range probed.Streams {
		if v.CodecType == "audio" {
			info.audioIndex = k
			break
		}
	}
	if info.audioIndex == -1 {
		fr.warning.Print("Non-audio file:", info.path)
		return errNonAudio
	}

	info.tags = make(map[string]string)
	info.filetags = make(map[string]string)

	// Precedence: cuesheet > stream tags > format tags.
	for k, v := range probed.Format.Tags {
		info.filetags[strings.ToLower(k)] = v
	}
	for k, v := range probed.Streams[info.audioIndex].Tags {
		key := strings.ToLower(k)
		_, ok := info.filetags[key]
		if !ok || info.filetags[key] == "" {
			info.filetags[key] = v
		}
	}

	var ErrCuesheet error
	info.cuesheet, ErrCuesheet = cuesheet.New(info.filetags["cuesheet"])
	if err != nil {
		// If no cuesheet was found in the tags, we check for external ones.
		pathNoext := StripExt(info.path)
		// Instead of checking the extension of files in current folder, we check
		// if a file with the 'cue' extension exists. This is faster, especially
		// for huge folders.
		for _, ext := range []string{"cue", "cuE", "cUe", "cUE", "Cue", "CuE", "CUe", "CUE"} {
			cs := pathNoext + "." + ext
			st, err := os.Stat(cs)
			if err != nil {
				continue
			}
			if st.Size() > cuesheetMaxsize {
				fr.warning.Printf("Cuesheet size %v > %v bytes, skipping", cs, cuesheetMaxsize)
				continue
			}
			buf, err := ioutil.ReadFile(cs)
			if err != nil {
				fr.warning.Print(err)
				continue
			}

			info.cuesheet, ErrCuesheet = cuesheet.New(string(buf))
			break
		}
	}
	// Remove cuesheet from tags to avoid printing it.
	delete(info.filetags, "cuesheet")

	// The number of tracks in current file is usually 1, it can be more if a
	// cuesheet is found.
	info.trackCount = 1
	if ErrCuesheet == nil {
		// Copy the cuesheet header to the tags. Some entries appear both in the
		// header and in the track details. We map the cuesheet header entries to
		// the respective quivalent for FFmpeg tags.
		for k, v := range info.cuesheet.Header {
			switch k {
			case "PERFORMER":
				info.filetags["album_artist"] = v
			case "SONGWRITER":
				info.filetags["album_artist"] = v
			case "TITLE":
				info.filetags["album"] = v
			default:
				info.filetags[strings.ToLower(k)] = v
			}
		}

		// A cuesheet might have several FILE entries, or even none (non-standard).
		// In case of none, tracks are stored at file "" (the empty string) in the
		// Cuesheet structure. Otherwise, we find the most related file.
		base := stringNorm(filepath.Base(info.path))
		max := 0.0
		for f := range info.cuesheet.Files {
			r := stringRel(stringNorm(f), base)
			if r > max {
				max = r
				info.cuesheetFile = f
			}
		}
		info.trackCount = len(info.cuesheet.Files[info.cuesheetFile])
	}

	// Set bitrate.
	// FFmpeg stores bitrate as a string, Demlo needs a number. If
	// 'streams[audioIndex].bit_rate' is empty (e.g. in APE files), look for
	// 'format.bit_rate'. To ease querying bitrate from user scripts, store it
	// in 'info.bitrate'.
	info.bitrate, err = strconv.Atoi(probed.Streams[info.audioIndex].Bitrate)
	if err != nil {
		info.bitrate, err = strconv.Atoi(probed.Format.Bitrate)
		if err != nil {
			fr.warning.Print("Cannot get bitrate from", info.path)
			return err
		}
	}

	return nil
}

func getEmbeddedCover(fr *FileRecord) {
	input := &fr.input

	// FFmpeg treats embedded covers like video streams.
	for i := 0; i < fr.Format.NbStreams; i++ {
		if fr.Streams[i].CodecName != "image2" &&
			fr.Streams[i].CodecName != "png" &&
			fr.Streams[i].CodecName != "mjpeg" {
			continue
		}

		cmd := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-y", "-i", input.path, "-an", "-sn", "-c:v", "copy", "-f", "image2", "-map", "0:"+strconv.Itoa(i), "-")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		cover, err := cmd.Output()
		if err != nil {
			fr.error.Printf(stderr.String())
			continue
		}

		reader := bytes.NewBuffer(cover)
		config, format, err := image.DecodeConfig(reader)
		if err != nil {
			fr.warning.Print(err)
			continue
		}

		hi := len(cover)
		if hi > coverChecksumBlock {
			hi = coverChecksumBlock
		}
		checksum := fmt.Sprintf("%x", md5.Sum(cover[:hi]))

		fr.embeddedCoverCache = append(fr.embeddedCoverCache, cover)
		input.embeddedCovers = append(input.embeddedCovers, inputCover{format: format, width: config.Width, height: config.Height, checksum: checksum})
	}
}

func getExternalCover(fr *FileRecord) error {
	// TODO: Memoize external cover queries.
	input := &fr.input
	fd, err := os.Open(filepath.Dir(input.path))
	if err != nil {
		return err
	}
	names, err := fd.Readdirnames(-1)
	fd.Close()
	if err != nil {
		return err
	}

	input.externalCovers = make(map[string]inputCover)

	for _, f := range names {
		if !coverExtList[strings.ToLower(Ext(f))] {
			continue
		}
		fd, err := os.Open(filepath.Join(filepath.Dir(input.path), f))
		if err != nil {
			fr.warning.Print(err)
			continue
		}
		defer fd.Close()

		st, err := fd.Stat()
		if err != nil {
			fr.warning.Print(err)
			continue
		}

		config, format, err := image.DecodeConfig(fd)
		if err != nil {
			fr.warning.Print(err)
			continue
		}

		hi := st.Size()
		if hi > coverChecksumBlock {
			hi = coverChecksumBlock
		}

		buf := [coverChecksumBlock]byte{}
		_, err = (*fd).ReadAt(buf[:hi], 0)
		if err != nil && err != io.EOF {
			fr.warning.Print(err)
			continue
		}
		checksum := fmt.Sprintf("%x", md5.Sum(buf[:hi]))

		input.externalCovers[f] = inputCover{format: format, width: config.Width, height: config.Height, checksum: checksum}
	}

	return nil
}

func prepareTrackTags(input *inputInfo, track int) {
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

// The format is:
//   [input] | attr | [output]
func prettyPrint(fr *FileRecord, attr, input, output string, attrMaxlen, valueMaxlen int) {
	colorIn := ""
	colorOut := ""
	if options.Color && input != output &&
		(attr != "parameters" || output != "[-c:a copy]") &&
		((attr != "embedded" && attr != "external") || (len(output) >= 3 && output[len(output)-3:] != " ''")) {
		colorIn = "red"
		colorOut = "green"
	}

	// Replace control characters to avoid mangling the output.
	input = rePrintable.ReplaceAllString(input, " / ")
	output = rePrintable.ReplaceAllString(output, " / ")

	in := []rune(input)
	out := []rune(output)

	min := func(a, b int) int {
		if a < b {
			return a
		}
		return b
	}

	// Print first line with title.
	fr.plain.Printf(
		"%*v["+ansi.Color("%.*s", colorIn)+"] | %-*v | ["+ansi.Color("%.*s", colorOut)+"]\n",
		valueMaxlen-min(valueMaxlen, len(in)), "",
		valueMaxlen, input,
		attrMaxlen, attr,
		valueMaxlen, output)

	// Print the rest that does not fit on first line.
	for i := valueMaxlen; i < len(in) || i < len(out); i += valueMaxlen {
		inLo := min(i, len(in))
		inHi := min(i+valueMaxlen, len(in))
		outLo := min(i, len(out))
		outHi := min(i+valueMaxlen, len(out))

		inDelimLeft, inDelimRight := "[", "]"
		outDelimLeft, outDelimRight := "[", "]"
		if i >= len(in) {
			inDelimLeft, inDelimRight = " ", " "
		}
		if i >= len(out) {
			outDelimLeft, outDelimRight = "", ""
		}

		fr.plain.Printf(
			"%s"+ansi.Color("%s", colorIn)+"%s%*v | %*v | %s"+ansi.Color("%s", colorOut)+"%s\n",
			inDelimLeft,
			string(in[inLo:inHi]),
			inDelimRight,
			valueMaxlen-inHi+inLo, "",
			attrMaxlen, "",
			outDelimLeft,
			string(out[outLo:outHi]),
			outDelimRight)
	}
}

func preview(fr *FileRecord, track int) {
	input := &fr.input
	output := &fr.output[track]

	maxCols, _, err := TerminalSize(int(os.Stderr.Fd()))
	if err != nil {
		// Can this happen? It would mean that os.Stderr has changed during
		// execution since we did the TerminalSize() check in main().
		return
	}

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
	if options.Color {
		colorTitle = "white+b"
	}

	fr.plain.Println()

	fr.plain.Printf("%*v === "+ansi.Color("%-*v", colorTitle)+" ===\n",
		valueMaxlen, "",
		attrMaxlen, "FILE")
	prettyPrint(fr, "path", input.path, output.Path, attrMaxlen, valueMaxlen)
	prettyPrint(fr, "format", fr.Format.FormatName, output.Format, attrMaxlen, valueMaxlen)
	prettyPrint(fr, "parameters", "bitrate="+strconv.Itoa(input.bitrate), fmt.Sprintf("%v", output.Parameters), attrMaxlen, valueMaxlen)

	fr.plain.Printf("%*v === "+ansi.Color("%-*v", colorTitle)+" ===\n",
		valueMaxlen, "",
		attrMaxlen, "TAGS")
	for _, v := range tagList {
		// "encoder" is a field that is usually out of control, discard it.
		if v != "encoder" {
			prettyPrint(fr, v, input.tags[v], output.Tags[v], attrMaxlen, valueMaxlen)
		}
	}

	fr.plain.Printf("%*v === "+ansi.Color("%-*v", colorTitle)+" ===\n",
		valueMaxlen, "",
		attrMaxlen, "COVERS")
	for stream, cover := range input.embeddedCovers {
		in := fmt.Sprintf("'stream %v' [%vx%v] <%v>", stream, cover.width, cover.height, cover.format)
		out := "<> [] ''"
		if stream < len(output.EmbeddedCovers) {
			out = fmt.Sprintf("<%v> %q '%v'", output.EmbeddedCovers[stream].Format, output.EmbeddedCovers[stream].Parameters, output.EmbeddedCovers[stream].Path)
		}
		prettyPrint(fr, "embedded", in, out, attrMaxlen, valueMaxlen)
	}
	for file, cover := range input.externalCovers {
		in := fmt.Sprintf("'%v' [%vx%v] <%v>", file, cover.width, cover.height, cover.format)
		out := fmt.Sprintf("<%v> %q '%v'", output.ExternalCovers[file].Format, output.ExternalCovers[file].Parameters, output.ExternalCovers[file].Path)
		prettyPrint(fr, "external", in, out, attrMaxlen, valueMaxlen)
	}
	if input.onlineCover.format != "" {
		cover := input.onlineCover
		in := fmt.Sprintf("[%vx%v] <%v>", cover.width, cover.height, cover.format)
		out := fmt.Sprintf("<%v> %q '%v'", output.OnlineCover.Format, output.OnlineCover.Parameters, output.OnlineCover.Path)
		prettyPrint(fr, "online", in, out, attrMaxlen, valueMaxlen)
	}

	fr.plain.Println()
}
