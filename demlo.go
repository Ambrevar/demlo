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

	"bitbucket.org/ambrevar/demlo/cuesheet"
	"github.com/mgutz/ansi"
)

const (
	application = "demlo"
	copyright   = "Copyright (C) 2013-2016 Pierre Neidhardt"
	URL         = "http://ambrevar.bitbucket.org/demlo"
)

var version = "<tip>"

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
	XDG_DATA_DIRS   = os.Getenv("XDG_DATA_DIRS")

	systemScriptRoot string
	userScriptRoot   string
	config           string

	warning = log.New(os.Stderr, ":: Warning: ", 0)

	previewOptions = struct {
		printIndex bool
		printDiff  bool
	}{false, true}

	cache = struct {
		index   map[string][]outputInfo
		scripts []scriptBuffer
	}{}

	// Options used in the config file and/or as CLI flags.
	// Precedence: flags > config > defaults.
	// Exception: extensions specified in flags are merged with config extensions.
	options = struct {
		Color        bool
		Cores        int
		Debug        bool
		Extensions   stringSetFlag
		Getcover     bool
		Gettags      bool
		Index        string
		Postscript   string
		Prescript    string
		Process      bool
		Removesource bool
		Scripts      []string
	}{}
)

// Identify visited cover files with {path,checksum} as map key.
type dstCoverKey struct {
	path     string
	checksum string
}

// scriptBuffer holds a script in memory.
// 'path' is stored for logging.
type scriptBuffer struct {
	path string
	buf  string
}

// scriptBufferSlice holds all the scripts to be called over each input file.
// It can be sorted in the lexicographic order of the script basenames.
type scriptBufferSlice []scriptBuffer

func (s scriptBufferSlice) Len() int { return len(s) }
func (s scriptBufferSlice) Less(i, j int) bool {
	return filepath.Base(s[i].path) < filepath.Base(s[j].path)
}
func (s scriptBufferSlice) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

// Store the names of the scripts to load later on.
type scriptAddFlag struct {
	names *[]string
}

func (s *scriptAddFlag) String() string {
	return fmt.Sprintf("%q", *s.names)
}

func (s *scriptAddFlag) Set(arg string) error {
	*s.names = append(*s.names, arg)
	return nil
}

// Remove the script names matching a regexp.
// 'names' should point to the same slice as scriptAddFlag.
type scriptRemoveFlag struct {
	names *[]string
}

func (s *scriptRemoveFlag) String() string {
	return ""
}

// We only need to match the basename so that behaviour is clearer regarding script
// finding.
func (s *scriptRemoveFlag) Set(arg string) error {
	re, err := regexp.Compile(arg)
	if err != nil {
		return err
	}

	for i := 0; i < len(*s.names); {
		if re.MatchString(StripExt(filepath.Base((*s.names)[i]))) {
			if i < len(*s.names)-1 {
				*s.names = append((*s.names)[:i], (*s.names)[i+1:]...)
			} else {
				*s.names = (*s.names)[:i]
			}
		} else {
			i++
		}
	}

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
	Path       string   `lua:"path"`
	Format     string   `lua:"format"`
	Parameters []string `lua:"parameters"`
}

// inputInfo is contains all the file's metadata passed to the scripts.
// Format and Streams are set from FFprobe respective sections.
// We do not export other fields: if FFprobe output changes, it could lead to
// undesired field overwriting.
type inputInfo struct {
	path    string // Realpath.
	bitrate int    // In bytes per second.
	tags    map[string]string

	embeddedCovers []inputCover          `lua:"embeddedcovers"`
	externalCovers map[string]inputCover `lua:"externalcovers"`
	onlineCover    inputCover            `lua:"onlinecover"`

	// Index of the first audio stream.
	audioIndex int

	// FFmpeg data.
	Format  map[string]interface{}   `lua:"format"`
	Streams []map[string]interface{} `lua:"streams"`

	// The following details for multi-track files are not transferred to Lua.
	filetags map[string]string
	cuesheet cuesheet.Cuesheet
	// Name of the matching file in the cuesheet.
	cuesheetFile string `lua:"cuesheetfile"`
	trackCount   int    `lua:"trackcount"`
}

// We could store everything in 'parameters', but having a separate 'path' and
// 'format' allows for foolproofing.
type outputInfo struct {
	Path           string                 `lua:"path"`
	Format         string                 `lua:"format"`
	Parameters     []string               `lua:"parameters"`
	Tags           map[string]string      `lua:"tags"`
	EmbeddedCovers []outputCover          `lua:"embeddedcovers"`
	ExternalCovers map[string]outputCover `lua:"externalcovers"`
	OnlineCover    outputCover            `lua:"onlinecover"`
}

// FileRecord holds the data passed through the pipeline.
// It contains, for one file, the input metadata and the output changes from the
// scripts. FFprobe's Format and Streams are fully stored in 'input' as
// interfaces that can be accessed directly from the script.
// It also contains:
// - Some file specific cache.
// - File specific loggers. (To guarantee the log messages won't be split.)
// - The needed bit of the 'Format' and 'Streams' sections from FFprobe,
//   unwrapped from any interface and thus properly typed.
type FileRecord struct {
	input  inputInfo
	output []outputInfo

	Format struct {
		Bitrate    string `json:"bit_rate"`
		Duration   string
		FormatName string `json:"format_name"`
		NbStreams  int    `json:"nb_streams"`
		Tags       map[string]string
	}
	Streams []struct {
		Bitrate   string `json:"bit_rate"`
		CodecName string `json:"codec_name"`
		CodecType string `json:"codec_type"`
		Duration  string
		Height    int
		Tags      map[string]string
		Width     int
	}

	embeddedCoverCache [][]byte
	onlineCoverCache   []byte

	debug   *log.Logger
	info    *log.Logger
	plain   *log.Logger
	section *log.Logger
	warning *log.Logger
	error   *log.Logger

	logBuf bytes.Buffer
}

func (f *FileRecord) String() string {
	return f.logBuf.String()
}

func newFileRecord(path string) *FileRecord {
	fr := FileRecord{}
	fr.input.path = path

	fr.debug = log.New(ioutil.Discard, "@@ ", 0)
	fr.info = log.New(&fr.logBuf, ":: ", 0)
	fr.plain = log.New(&fr.logBuf, "", 0)
	fr.section = log.New(&fr.logBuf, "==> ", 0)
	fr.warning = log.New(&fr.logBuf, ":: Warning: ", 0)
	fr.error = log.New(&fr.logBuf, ":: Error: ", 0)

	if options.Debug {
		fr.debug.SetOutput(&fr.logBuf)
	}

	if options.Color {
		fr.debug.SetPrefix(ansi.Color(fr.debug.Prefix(), "cyan+b"))
		fr.info.SetPrefix(ansi.Color(fr.info.Prefix(), "magenta+b"))
		fr.section.SetPrefix(ansi.Color(fr.section.Prefix(), "green+b"))
		fr.warning.SetPrefix(ansi.Color(fr.warning.Prefix(), "yellow+b"))
		fr.error.SetPrefix(ansi.Color(fr.error.Prefix(), "red+b"))
	}

	return &fr
}

// findScript returns the path of the first script found with name 'name'.
// It looks up, by order of precedence ('.' is the current folder):
//
//   . > userScriptRoot > systemScriptRoot
//
// A script name from the config file can target a file found in current folder.
// This choice makes it possible to replace a system/user script without
// additional command-line parameters. Besides, since scripts are sorted by
// basename, several identical basenames could lead to an unstable sort order.
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
	return "", nil, errors.New("script not found")
}

func printExtensions() {
	extlist := make([]string, 0, len(options.Extensions))
	for k := range options.Extensions {
		extlist = append(extlist, k)
	}
	sort.StringSlice(extlist).Sort()
	log.Printf("Register extensions: %v", strings.Join(extlist, " "))
}

func printScripts() {
	f := func(name, folder string) {
		log.Printf("%v script folder: %v", name, folder)
		f, err := os.Open(folder)
		if err != nil {
			if !os.IsNotExist(err) {
				warning.Print(err)
			}
			return
		}
		defer f.Close()

		dn, err := f.Readdirnames(0)
		if err != nil {
			warning.Print(err)
			return
		}
		sort.StringSlice(dn).Sort()
		log.Printf("%v scripts: %q", name, dn)
	}

	f("System", systemScriptRoot)
	f("User", userScriptRoot)
}

func cacheScripts() {
	visited := map[string]bool{}
	for _, s := range options.Scripts {
		path, st, err := findScript(s)
		if err != nil {
			warning.Printf("%v: %v", err, s)
			continue
		}
		if visited[path] {
			continue
		}
		visited[path] = true
		if sz := st.Size(); sz > scriptMaxsize {
			warning.Printf("Script size %v > %v bytes, skipping: %v", sz, scriptMaxsize, path)
			continue
		}
		buf, err := ioutil.ReadFile(path)
		if err != nil {
			warning.Print("Script is not readable: ", err)
			continue
		}
		cache.scripts = append(cache.scripts, scriptBuffer{path: path, buf: string(buf)})
	}

	sort.Sort(scriptBufferSlice(cache.scripts))
	for _, s := range cache.scripts {
		log.Printf("Load script: %v", s.path)
	}

	if options.Prescript != "" {
		cache.scripts = append([]scriptBuffer{{path: "prescript", buf: options.Prescript}}, cache.scripts...)
	}
	if options.Postscript != "" {
		cache.scripts = append(cache.scripts, scriptBuffer{path: "postscript", buf: options.Postscript})
	}
}

func cacheIndex() {
	if options.Index != "" {
		st, err := os.Stat(options.Index)
		if err != nil {
			warning.Printf("Index not found: [%v]", options.Index)
		} else if st.Size() > indexMaxsize {
			warning.Printf("Index size > %v bytes, skipping: %v", indexMaxsize, options.Index)
		} else if buf, err := ioutil.ReadFile(options.Index); err != nil {
			warning.Print("Index is not readable:", err)
		} else {
			// Enclose JSON list in a valid structure: index ends with a
			// comma, hence the required dummy entry.
			buf = append(append([]byte{'{'}, buf...), []byte(`"": null}`)...)
			err = json.Unmarshal(buf, &cache.index)
			if err != nil {
				warning.Printf("Invalid index %v: %v", options.Index, err)
			}
		}
	}
}

// Note to packagers: those following lines can be patched to fit the local
// filesystem.
func init() {
	log.SetFlags(0)
	log.SetPrefix(":: ")

	if XDG_CONFIG_HOME == "" {
		XDG_CONFIG_HOME = filepath.Join(os.Getenv("HOME"), ".config")
	}

	if XDG_DATA_DIRS == "" {
		XDG_DATA_DIRS = "/usr/local/share/:/usr/share"
	}

	findInPath := func(pathlist, subpath string) string {
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

	systemScriptRoot = findInPath(XDG_DATA_DIRS, filepath.Join(application, "scripts"))
	userScriptRoot = findInPath(XDG_CONFIG_HOME, filepath.Join(application, "scripts"))

	config = os.Getenv("DEMLORC")
	if config == "" {
		config = filepath.Join(XDG_CONFIG_HOME, application, application+"rc")
	}
}

func main() {
	// Load config first since it changes the default flag values.
	st, err := os.Stat(config)
	if err == nil && st.Mode().IsRegular() {
		log.Printf("Load config: %v", config)
		LoadConfig(config, &options)
	}
	if options.Extensions == nil {
		// Defaults: Init here so that unspecified config options get properly set.
		options.Extensions = stringSetFlag{
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

	flag.BoolVar(&options.Color, "color", options.Color, "Color output.")
	flag.IntVar(&options.Cores, "cores", options.Cores, "Run N processes in parallel. If 0, use all online cores.")
	flag.BoolVar(&options.Debug, "debug", false, "Enable debug messages.")
	flag.Var(&options.Extensions, "ext", "Additional extensions to look for when a folder is browsed.")
	flag.BoolVar(&options.Getcover, "c", options.Getcover, "Fetch cover from the Internet.")
	flag.BoolVar(&options.Gettags, "t", options.Gettags, "Fetch tags from the Internet.")
	flag.StringVar(&options.Index, "i", options.Index, `Use index file to set input and output metadata.
    	The index can be built using the non-formatted preview output.`)
	flag.StringVar(&options.Postscript, "post", options.Postscript, "Run Lua commands after the other scripts.")
	flag.StringVar(&options.Prescript, "pre", options.Prescript, "Run Lua commands before the other scripts.")
	flag.BoolVar(&options.Process, "p", options.Process, "Apply changes: set tags and format, move/copy result to destination file.")
	flag.BoolVar(&options.Removesource, "rmsrc", options.Removesource, "Remove source file after processing.")

	sFlag := scriptAddFlag{&options.Scripts}
	flag.Var(&sFlag, "s", `Specify scripts to run in lexicographical order.
    	This option can be specified several times. The path and the extension can be omitted.
    	The current folder, the user script folder and the system script folder are search in this order.`)

	rFlag := scriptRemoveFlag{&options.Scripts}
	flag.Var(&rFlag, "r", `Remove scripts where the regexp matches a part of the basename.
    	An empty regexp removes all scripts.`)

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

	// Enable index output if stdout is redirected.
	st, _ = os.Stdout.Stat()
	if (st.Mode() & os.ModeCharDevice) == 0 {
		previewOptions.printIndex = true
	}
	// Disable diff preview if stderr is does not have a 'TerminalSize'.
	st, _ = os.Stderr.Stat()
	if (st.Mode() & os.ModeCharDevice) == 0 {
		options.Color = false
		previewOptions.printDiff = false
	} else if _, _, err := TerminalSize(int(os.Stderr.Fd())); err != nil {
		options.Color = false
		previewOptions.printDiff = false
	}

	if options.Color {
		log.SetPrefix(ansi.Color(log.Prefix(), "magenta+b"))
		warning.SetPrefix(ansi.Color(warning.Prefix(), "yellow+b"))
	}

	printExtensions()
	printScripts()
	cacheScripts()
	cacheIndex()

	// Limit number of cores to online cores.
	if options.Cores > runtime.NumCPU() || options.Cores <= 0 {
		options.Cores = runtime.NumCPU()
	}

	// Pipeline.
	// The log queue should be able to hold all routines at once.
	p := NewPipeline(1, 1+options.Cores+options.Cores)

	p.Add(func() Stage { return &walker{} }, 1)
	p.Add(func() Stage { return &analyzer{} }, options.Cores)

	if options.Process {
		p.Add(func() Stage { return &transformer{} }, options.Cores)
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
