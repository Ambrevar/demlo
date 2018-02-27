# Demlo - a dynamic and extensible music library organizer

Demlo is a music library organizer. It can encode, fix case, change folder
hierarchy according to tags or file properties, tag from an online database,
copy covers while ignoring duplicates or those below a quality threshold, and
much more. It makes it possible to manage your libraries uniformly and
dynamically. You can write your own rules to fit your needs best.

Demlo can address any of these recurring music library issues (and much more):

- Fix the lack of folder structure.
- Normalize tags, fix their case, chose which tags to keep and which to discard.
- Handle lossy and lossless audio differently.
- Handle mp3 id3tags hell…
- Handle multiple covers, whether embedded and/or external, resize covers,
discard bad quality ones.


## Preview

Here follows a sample output showing the "before-after" differences.

	$ demlo fantasie_impromptu.flac
	:: Load config: /home/johndoe/.config/demlo/config.lua
	:: Load script 10-tag-normalize: /usr/share/demlo/scripts/10-tag-normalize.lua
	:: Load script 20-tag-replace: /usr/share/demlo/scripts/20-tag-replace.lua
	:: Load script 30-tag-case: /usr/share/demlo/scripts/30-tag-case.lua
	:: Load script 40-tag-punctuation: /usr/share/demlo/scripts/40-tag-punctuation.lua
	:: Load script 50-encoding: /usr/share/demlo/scripts/50-encoding.lua
	:: Load script 51-encoding-flac2ogg: /home/johndoe/.config/demlo/scripts/51-encoding-flac2ogg.lua
	:: Load script 60-path: /usr/share/demlo/scripts/60-path.lua
	:: Load script 70-cover: /usr/share/demlo/scripts/70-cover.lua
	==> fantasie_impromptu.flac

	                                               === FILE         ===
	         [/home/johndoe/fantasie_impromptu.flac] | path         | [/home/johndoe/music/Chopin/The Best Ever Piano ]
	                                                 |              | [Classics (John Doe, 2014)/Fantasie-Impromptu in]
	                                                 |              | [ C Sharp Minor, Op. 66.ogg]
	                                          [flac] | format       | [ogg]
	                                [bitrate=320000] | parameters   | [[-c:a libvorbis -q:a 10]]
	                                               === TAGS         ===
	            [john doe's classical collection II] | album        | [John Doe's Classical Collection II]
	                                              [] | album_artist | [Chopin]
	                                              [] | artist       | [Chopin]
	                                        [chopin] | composer     | []
	                                    [02/13/2014] | date         | [2014]
	                                      [Classics] | genre        | [Classical]
	                                     [John_Doe ] | performer    | [John Doe]
	   [Fantasie-Impromptu in c sharp MInor , Op.66] | title        | [Fantasie-Impromptu in C Sharp Minor, Op. 66]
	                                               === COVERS       ===
	                  ['cover.jpg' [500x500] <jpeg>] | external     | [/home/johndoe/music/Chopin/The Best Ever Piano ]
	                                                 |              | [Classics (John Doe, 2014)/Cover.jpg]



## Installation

### Packages

- [Arch Linux package (AUR)](https://aur.archlinux.org/packages/demlo-git/)


### Manual

Compile-time dependencies:

* Go
* Lua (≥5.1)
* TagLib

Runtime dependencies:

* FFmpeg (with ffprobe, preferably latest version)

Optional dependencies:

* fpcalc (from chromaprint, to query tags online)

Set up a Go environment (see <https://golang.org/doc/install>) and run:

	$ go get github.com/ambrevar/demlo

The version number is set at compilation time. To package a specific version,
checkout the corresponding tag and set `version` from the build command, e.g.:

	go build -ldflags "-X main.version=r$(git rev-list --count HEAD).$(git describe --tags --always).$(git log -1 --format="%cd" --date=short)"

or simply

	go build -ldflags "-X main.version=$(git describe --tags --always)"

To build statically (assuming you have all the required static libraries at hand):

	go build -ldflags '-extldflags "-static -ldl -lm -lz -lstdc++"'

Install the files as follows:

	demlo   -> /usr/{local/}bin/demlo
	config.lua -> /usr/{local/}share/demlo/config.lua
	scripts/ -> /usr/{local/}share/demlo/scripts/

## Usage

See `demlo`, `demlo -help` and `demlo -h <script-name>` for contextual help from
the commandline.

## Breaking changes

### 3.8

- Renamed `demlorc` to `config.lua`.  System configuration is loaded if
  user configuration is not found.

- Renamed configuration environment variable `DEMLORC` to `DEMLO_CONFIG`.

- Renamed script `90-rmsrc` to `90-remove_source`.

- Namespaced "tag" scripts.

- Replaced `-I` commandline argument with `-o` to write index files directly.

- Some script functions were changed.
