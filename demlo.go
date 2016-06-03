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
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"

	"bitbucket.org/ambrevar/demlo/cuesheet"
	"github.com/mgutz/ansi"
)

const (
	application = "demlo"
	copyright   = "Copyright (C) 2013-2016 Pierre Neidhardt"
	URL         = "http://ambrevar.bitbucket.org/demlo"
)

var version = "tip"

const usage = `Batch-transcode files with user-written scripts for dynamic tagging
and encoding.

Folders are processed recursively. Only files with known extensions are processed.
New extensions can be specified from command-line.

All flags that do not require an argument are booleans. Without argument, they
take the true value. To negate them, use the form '-flag=false'.

See ` + URL + ` for more details.
`

const (
	// coverChecksumBlock limits cover checksums to this amount of bytes for performance gain.
	coverChecksumBlock = 8 * 4096
	// 10M seems to be a reasonable max.
	cuesheetMaxsize = 10 * 1024 * 1024
	indexMaxsize    = 10 * 1024 * 1024
	scriptMaxsize   = 10 * 1024 * 1024
)

var (
	XDG_CONFIG_HOME = os.Getenv("XDG_CONFIG_HOME")
	XDG_CONFIG_DIRS = os.Getenv("XDG_CONFIG_DIRS")
	XDG_DATA_DIRS   = os.Getenv("XDG_DATA_DIRS")

	systemScriptRoot string
	userScriptRoot   string
	config           string

	coverExtList = map[string]bool{"gif": true, "jpeg": true, "jpg": true, "png": true}

	options = optionSet{}

	previewOptions = struct {
		printIndex bool
		printDiff  bool
	}{false, true}

	cache = struct {
		index   map[string][]outputInfo
		scripts []scriptBuffer
	}{}

	rePrintable = regexp.MustCompile(`\pC`)

	visitedDstCovers = struct {
		v map[dstCoverKey]bool
		sync.RWMutex
	}{v: map[dstCoverKey]bool{}}

	errInputFile = errors.New("Cannot process input file")
)

// Identify visited cover files with {path,checksum} as map key.
type dstCoverKey struct {
	path     string
	checksum string
}

// Options used in the config file and/or as CLI flags.
// TODO: Can we use an anonymous structure?
// Precedence: flags > config > defaults.
// Exception: extensions specified in flags are merged with config extensions.
type optionSet struct {
	color        bool
	cores        int
	debug        bool
	extensions   stringSetFlag
	getcover     bool
	gettags      bool
	index        string
	overwrite    bool
	postscript   string
	prescript    string
	process      bool
	removesource bool
	scripts      scriptSlice
}

// Load scripts in memory to reduce I/O.
// We need to store the script name as well for logging.
type scriptBuffer struct {
	name string
	buf  string
}

// Scripts specified from commandline.
type scriptSlice []string

func (s *scriptSlice) String() string {
	// Print the default/config value.
	return fmt.Sprintf("%q", options.scripts)
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
type inputInfo struct {
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
		Bitrate   string `json:"bit_rate"`
		CodecName string `json:"codec_name"`
		CodecType string `json:"codec_type"`
		Duration  string
		Height    int
		Tags      map[string]string
		Width     int
	}
	Format struct {
		Bitrate    string `json:"bit_rate"`
		Duration   string
		FormatName string `json:"format_name"`
		NbStreams  int    `json:"nb_streams"`
		Tags       map[string]string
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
type outputInfo struct {
	Path           string
	Format         string
	Parameters     []string
	Tags           map[string]string
	EmbeddedCovers []outputCover
	ExternalCovers map[string]outputCover
	OnlineCover    outputCover
}

// FileRecord holds the data passed through the pipeline.
// It contains, for one file, the input metadata, the output changes, some cache, and the logger.
type FileRecord struct {
	input  inputInfo
	output []outputInfo

	embeddedCoverCache [][]byte
	onlineCoverCache   []byte

	Debug   *log.Logger
	Info    *log.Logger
	Output  *log.Logger
	Section *log.Logger
	Warning *log.Logger
	Error   *log.Logger

	logBuf bytes.Buffer
}

func (f *FileRecord) String() string {
	return f.logBuf.String()
}

func newFileRecord(path string) *FileRecord {
	fr := FileRecord{}
	fr.input.path = path

	fr.Debug = log.New(ioutil.Discard, "@@ ", 0)
	fr.Info = log.New(&fr.logBuf, ":: ", 0)
	fr.Output = log.New(&fr.logBuf, "", 0)
	fr.Section = log.New(&fr.logBuf, "==> ", 0)
	fr.Warning = log.New(&fr.logBuf, ":: Warning: ", 0)
	fr.Error = log.New(&fr.logBuf, ":: Error: ", 0)

	if options.debug {
		fr.Debug.SetOutput(&fr.logBuf)
	}

	if options.color {
		fr.Debug.SetPrefix(ansi.Color(fr.Debug.Prefix(), "cyan+b"))
		fr.Info.SetPrefix(ansi.Color(fr.Info.Prefix(), "magenta+b"))
		fr.Section.SetPrefix(ansi.Color(fr.Section.Prefix(), "green+b"))
		fr.Warning.SetPrefix(ansi.Color(fr.Warning.Prefix(), "blue+b"))
		fr.Error.SetPrefix(ansi.Color(fr.Error.Prefix(), "red+b"))
	}

	return &fr
}

// Return the first existing match from 'list'.
func findScript(name string) (path string, st os.FileInfo, err error) {
	nameExt := name + ".lua"
	list := []string{
		name,
		nameExt,
		filepath.Join(userScriptRoot, name),
		filepath.Join(userScriptRoot, nameExt),
		filepath.Join(systemScriptRoot, name),
		filepath.Join(systemScriptRoot, nameExt),
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

	systemScriptRoot = pathlistSub(XDG_DATA_DIRS, filepath.Join(application, "scripts"))
	userScriptRoot = pathlistSub(XDG_CONFIG_HOME, filepath.Join(application, "scripts"))

	config = os.Getenv("DEMLORC")
	if config == "" {
		config = filepath.Join(XDG_CONFIG_HOME, application, application+"rc")
	}
}

func main() {
	// Load config first since it changes the default flag values.
	st, err := os.Stat(config)
	if err == nil && st.Mode().IsRegular() {
		fmt.Fprintf(os.Stderr, ":: Load config: %v\n", config)
		options = loadConfig(config)
	}
	if options.extensions == nil {
		// Defaults: Init here so that unspecified config options get properly set.
		options.extensions = stringSetFlag{
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

	flag.BoolVar(&options.color, "color", options.color, "Color output.")
	flag.IntVar(&options.cores, "cores", options.cores, "Run N processes in parallel. If 0, use all online cores.")
	flag.BoolVar(&options.debug, "debug", false, "Enable debug messages.")
	flag.Var(&options.extensions, "ext", "Additional extensions to look for when a folder is browsed.")
	flag.BoolVar(&options.getcover, "c", options.getcover, "Fetch cover from the Internet.")
	flag.BoolVar(&options.gettags, "t", options.gettags, "Fetch tags from the Internet.")
	flag.StringVar(&options.index, "i", options.index, `Use index file to set input and output metadata.
    	The index can be built using the non-formatted preview output.`)
	flag.StringVar(&options.postscript, "post", options.postscript, "Run Lua commands after the other scripts.")
	flag.StringVar(&options.prescript, "pre", options.prescript, "Run Lua commands before the other scripts.")
	flag.BoolVar(&options.process, "p", options.process, "Apply changes: set tags and format, move/copy result to destination file.")
	flag.BoolVar(&options.removesource, "rmsrc", options.removesource, "Remove source file after processing.")
	var flagScripts scriptSlice
	flag.Var(&flagScripts, "s", `Specify scripts to run in provided order.
    	This option can be specified several times. If only the basename without extension is given,
    	and if it is not found in current folder, the corresponding standard script will be used.`)

	var flagVersion = flag.Bool("v", false, "Print version and exit.")

	flag.Parse()

	if *flagVersion {
		fmt.Println(application, version, copyright)
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
		previewOptions.printIndex = true
	}
	st, _ = os.Stderr.Stat()
	if (st.Mode() & os.ModeCharDevice) == 0 {
		options.color = false
		previewOptions.printDiff = false
	}

	info := log.New(os.Stderr, ":: ", 0)
	warning := log.New(os.Stderr, ":: Warning: ", 0)
	if options.color {
		info.SetPrefix(ansi.Color(info.Prefix(), "magenta+b"))
		warning.SetPrefix(ansi.Color(warning.Prefix(), "blue+b"))
	}

	// Cache index.
	if options.index != "" {
		st, err := os.Stat(options.index)
		if err != nil {
			warning.Printf("Index not found: [%v]", options.index)
		} else if st.Size() > indexMaxsize {
			warning.Printf("Index size > %v bytes, skipping: %v", indexMaxsize, options.index)
		} else if buf, err := ioutil.ReadFile(options.index); err != nil {
			warning.Print("Index is not readable:", err)
		} else {
			// Enclose JSON list in a valid structure: index ends with a
			// comma, hence the required dummy entry.
			buf = append(append([]byte{'{'}, buf...), []byte(`"": null}`)...)
			err = json.Unmarshal(buf, &cache.index)
			if err != nil {
				warning.Printf("Invalid index %v: %v", options.index, err)
			}
		}
	}

	// Cache scripts.
	if options.prescript != "" {
		cache.scripts = append(cache.scripts, scriptBuffer{name: "prescript", buf: options.prescript})
	}
	if len(flagScripts) > 0 {
		// CLI overrides default/config values.
		options.scripts = flagScripts
	}
	for _, s := range options.scripts {
		path, st, err := findScript(s)
		if err != nil {
			warning.Printf("%v: %v", err, s)
			continue
		}
		if sz := st.Size(); sz > scriptMaxsize {
			warning.Printf("Script size %v > %v bytes, skipping: %v", sz, scriptMaxsize, path)
			continue
		}
		buf, err := ioutil.ReadFile(path)
		if err != nil {
			warning.Print("Script is not readable: ", err)
			continue
		}
		info.Printf("Load script: %v", path)
		cache.scripts = append(cache.scripts, scriptBuffer{name: path, buf: string(buf)})
	}
	if options.postscript != "" {
		cache.scripts = append(cache.scripts, scriptBuffer{name: "postscript", buf: options.postscript})
	}

	// Limit number of cores to online cores.
	if options.cores > runtime.NumCPU() || options.cores <= 0 {
		options.cores = runtime.NumCPU()
	}

	// Pipeline.
	// The log queue should be able to hold all routines at once.
	p := NewPipeline(1, 1+options.cores+options.cores)

	p.Add(func() Stage { return &walker{} }, 1)
	p.Add(func() Stage { return &analyzer{} }, options.cores)

	if options.process {
		p.Add(func() Stage { return &transformer{} }, options.cores)
	}

	// Produce pipeline input. This should be run in parallel to pipeline
	// consumption.
	go func() {
		for _, file := range flag.Args() {
			visit := func(path string, info os.FileInfo, err error) error {
				if err != nil || !info.Mode().IsRegular() {
					return nil
				}
				p.input <- newFileRecord(path)
				return nil
			}
			// 'visit' always keeps going, so no error.
			_ = filepath.Walk(file, visit)
		}
		close(p.input)
	}()

	// Consume pipeline output.
	for fr := range p.output {
		p.log <- fr
	}
	p.Close()
}
