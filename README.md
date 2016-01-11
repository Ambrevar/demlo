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

Copy the files as follows:

	demlo   -> /usr/{local/}bin/demlo
	demlorc -> /usr/{local/}share/demlo/demlorc
	scripts/ -> /usr/{local/}share/demlo/scripts/

## Usage

See `demlo -h` and demlo(1).

## License

See LICENSE.
