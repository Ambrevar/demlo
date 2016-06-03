# Demlo - a dynamic and extensible music library organizer

Demlo is a music library organizer. It can encode, fix case, change folder
hierarchy according to tags or file properties, tag from an online database,
copy covers while ignoring duplicates or those below a quality threshold, and
much more. It makes it possible to manage your libraries uniformly and
dynamically. You can write your own rules to fit your needs best.

Demlo aims to be as lightweight and portable as possible. Its only big
dependency is the transcoder FFmpeg. The scripts are written in Lua for
portability and speed while allowing virtually unlimited extensibility.

## Installation

Compile-time dependencies:

* Go
* Lua (≥5.1)
* TagLib

Runtime dependencies:

* FFmpeg (with ffprobe, preferably latest version)

Optional dependencies:

* fpcalc (from chromaprint, to query tags online)

Set up a Go environment (see <https://golang.org/doc/install>) and run:

	$ go get bitbucket.org/ambrevar/demlo

The version number is set at compilation time. To package a specific version,
checkout the corresponding tag and set `version` from the build command, e.g.:

	go build -ldflags "-X main.version=r$(git rev-list --count HEAD).$(git describe --tags --always).$(git log -1 --format="%cd" --date=short)"

or simply

	go build -ldflags "-X main.version=$(git describe --tags --always)"

To build statically (assuming you have all the required static libraries at hand):

	go build -ldflags '-extldflags "-static -ldl -lm -lz -lstdc++"'

Install the files as follows:

	demlo   -> /usr/{local/}bin/demlo
	demlorc -> /usr/{local/}share/demlo/demlorc
	scripts/ -> /usr/{local/}share/demlo/scripts/

## Usage

See `demlo -h` and the [home page](http://ambrevar.bitbucket.org/demlo/).

## License

See LICENSE.
