// Copyright © 2013-2016 Pierre Neidhardt <ambrevar@gmail.com>
// Use of this file is governed by the license that can be found in LICENSE.

/*
A dynamic and extensible music library organizer

Demlo is a music library organizer. It can encode, fix case, change folder
hierarchy according to tags or file properties, tag from an online database,
copy covers while ignoring duplicates or those below a quality threshold, and
much more. It makes it possible to manage your libraries uniformly and
dynamically. You can write your own rules to fit your needs best.

Demlo aims at being as lightweight and portable as possible. Its major
runtime dependency is the transcoder FFmpeg. The scripts are written in Lua for
portability and speed while allowing virtually unlimited extensibility.

Usage:

	demlo [OPTIONS] FILES...

For usage options, see:

	demlo -h



Process

First Demlo creates a list of all input files. When a folder is specified, all
files matching the extensions from the 'extensions' variable will be appended to
the list. Identifal files are appended only once.

Next all files gets analyzed:

- The audio file details (tags, stream properties, format properties, etc.) are
stored into the 'input' variable. The 'output' variable gets its default values
from 'input', or from an index file if specified from command-line. If no index
has been specified and if an attached cuesheet is found, all cuesheet details
are appended accordingly. Cuesheet tags override streams tags, which override
format tags. Finally, still without index, tags can be retrieved from Internet
if the command-line option is set.

- If a prescript has been specified, it gets executed. It makes it possible to
adjust the input values before the script gets executed.

- The scripts get executed in order, if any. The 'output' variable is
transformed accordingly. Scripts may contain rules such as defining a new file
name, new tags, new encoding properties, etc. You can use conditions on input
values to set the output properties, which makes it virtually possible to
process a full music library in one single run.

- If a postscript has been specified, it gets executed. It makes it possible to
adjust the output of the script for the current run only.

- Demlo makes some last-minute tweaking if need be: it adjusts the bitrate, the
path, the encoding parameters, and so on.

- A preview is displayed.

- If applying changes, the covers get copied if required and the audio file gets
processed: tags are modified as specified, the file is re-encoded if required,
and the output is written to the appropriate folder. When destination already
exists, a random suffix is added to the filename.



Configuration

The program's default behaviour can be changed from the user configuration file.
(See the 'Files' section for a template.) Most command-line flags default value
can be changed. The configuration file is loaded on startup, before parsing the
command-line options. Review the default value of the CLI flags with 'demlo -h'.

If you wish to use no configuration file, set the environment variable DEMLORC
to ".".



Scripts

Scripts can contain any Lua code that is considered secure. Some functions like
'os.execute' are not available for security reasons. It is not possible to print
to the standard output/error unless running in debug mode and using the 'debug'
function.

See the 'sandbox.go' file for a list of allowed functions and variables.

Lua patterns are replaced by Go regexps. See
https://github.com/google/re2/wiki/Syntax.

Scripts have no requirements at all. However, to be useful, they should set
values of the 'output' table detailed in the 'Variables' section. You can use
the full power of the Lua to set the variables dynamically. For instance:

	output.path = library .. '/' .. o.artist .. '/' .. (not empty(o.album) and o.album .. '/' or '') .. track_padded .. '. ' .. o.title

'input' and 'output' are both accessible from any script.

All default functions and variables (excluding 'output') are reset on every
script call to enforce consistency. Local variables are lost from one script
call to another. Global variables are preserved. Use this feature to pass data
like options or new functions.

'output' is guaranteed to be a table, but Demlo does not provide more
consistency check. Demlo will only extract the fields with the right type as
described in the 'Variables' section.

Warning: Do not abuse of global variables, especially when processing non-fixed
size data (e.g. tables). Data could grow big and slow down the program.



Runtime code

The user scripts should be generic. Therefore they may not properly handle some
uncommon input values. Tweak the input with temporary overrides from
command-line.

The prescript and postscript defined on command-line will let you run arbitrary
code that is run before and after all other scripts, respectively.

Use global variables to transfer data and parameters along.

If the prescript and postscript end up being too long, consider writing a demlo
script. You can also define shell aliases or use wrapper scripts to help.



Variables

The 'input' table describes the file:

	input = {
	   path = '/real/path',
	   bitrate = 0,
	   tags = {},
	   audioindex = 0,
	   streams = {},
	   format = {},
	   embeddedcovers = {},
	   externalcovers = {},
	   onlinecover = {},
	}

Bitrate is in bits per seconds (bps). That is, for 320 kbps you would specify
	output.bitrate = 320000

The entry 'streams' and 'format' are as returned by
	ffprobe -v quiet -print_format json -show_streams -show_format $file

If gives access to most metadata that FFmpeg can return. For instance, to get
the duration of the track in seconds, query the variable
'input.format.duration'.

Since there may be more than one stream (covers, other data), the first audio
stream is assumed to be the music stream. For convenience, the index of the
music stream is stored in 'audioindex'.

The tags returned by FFmpeg are found in streams, format, and in the cuesheet
can. To make tag queries easier, all tags are stored in the 'tags' table, with
the following precedence:

	format tags < stream tags < cuesheet header tags < cuesheet track tags

You can remove a tag by setting it to 'nil' or the empty string. This is
equivalent, except that 'nil' is saving some memory during the process.


The 'output' table describes the transformation to apply on the file:

	output = {
	   path = 'full/path/with.ext',
	   format = 'format',
	   parameters = {},
	   tags = {},
	   embeddedcovers = {},
	   externalcovers = {},
	   onlinecover = {},
	}

The 'parameters' array holds the CLI parameters passed to FFmpeg. It can be
anything supported by FFmpeg, although this variable is supposed to hold
encoding information. See the 'Examples' section.

The 'embeddedcovers', 'externalcovers' and 'onlinecover' variables are detailed
in the 'Covers' section.

For convenience, the following shortcuts are provided:
	i = input.tags
	o = output.tags



Functions

Some functions are available to the user to ease scripting.

	debug(string...)
Display a message on stderr if debug mode is on.

	stringnorm(string)
Return lowercase string without non-alphanumeric characters nor leading zeros.

	stringrel(string, string)
Return the relation coefficient of the two input strings. The result is a float
in 0.0...1.0, 0 means no relation at all, 1 means identical strings.



Encoding

A format is a container in FFmpeg's terminology.

'output.parameters' contains CLI flags passed to FFmpeg. They are meant to set
the stream codec, the bitrate, etc.

If 'output.parameters' is {"-c:a", "copy"} and the format is identical,
then taglib will be used instead of FFmpeg.

Use this rule from a (post)script to disable encoding by setting the same format
and the right parameters.



Preview

The official scripts are usually very smart about guessing the right values.
They might make mistakes however. If you are unsure, you can (and you are
advised to) preview the results before proceeding. The preview can be printed in
JSON format or in a more human-readable format depending on the options.



Internet service

The initial values of the 'output' table can be completed with tags fetched from
the Musicbrainz database. Audio files are fingerprinted for the queries, so even
with initially wrong names and tags, the right values should still be retrieved.
The front album cover can also be retrieved.

Proxy parameters will be fetched automatically from the 'http_proxy'
and 'https_proxy' environment variables.

As this process requires network access it can be quite slow. Nevertheless,
Demlo is specifically optimized for albums, so that network queries are
used for one track per album only, when possible.

Some tracks can be released on different albums: Demlo tries to guess it from
the tags, but if the tags are wrong there is no way to know which one it is.
There is a case where the selection can be controlled: let's assume we have
tracks A, B and C from the same album Z. A and B were also released in album Y,
whereas C was release in Z only.

	demlo -cores 1 -t A B C

Tags for A will be checked online; let's assume it gets tagged to album Y. B
will use A details, so album Y too. Then C does not match neither A's nor B's
album, so another online query will be made and it will be tagged to album Z.
This is slow and does not yield the expected result.

Now let's call

	demlo -cores 1 -t audio.file C A B

Tags for C will be queried online, and C will be tagged to Z. Then both A and B
will match album Z so they will be tagged using C details, which is the desired
result.

Conclusion: when using online tagging, the first argument should be the lesser
known track of the album.



Index

Demlo can set the output variables according to the values set in a text file
before calling the script. The input values are ignored as well as online
tagging, but it is still possible to access the input table from scripts. This
'index' file is formatted in JSON. It corresponds to what Demlo outputs when
printing the JSON preview. This is valid JSON except for the missing beginning
and the missing end. It makes it possible to concatenate and to append to
existing index files. Demlo will automatically complete the missing parts so
that it becomes valid JSON.

The index file is useful when you want to edit tags manually: You can redirect
the output to a file, edit the content manually with you favorite text editor,
then run Demlo again with the index as argument. See the 'Examples' section.

This feature can also be used to interface Demlo with other programs.



Covers

Demlo can manage embedded covers as well as external covers.

External covers are queried from files matching known extension in the file's
folder.
Embedded covers are queried from static video streams in the file.
Covers are accessed from

	input.embeddedcovers = {
		[<cover index>] = inputcover
	}
	input.externalcovers = {
		["cover basename"] = inputcover
	}
	input.onlinecover = inputcover

The embedded covers are indexed numerically by order of appearance in the
streams. The first cover will be at index 1 and so on. This is not necesarily
the index of the stream.

'inputcover' is the following structure:

	{
		format '', -- e.g. 'gif', 'jpeg', 'png'.
		width 0,
		height 0,
		checksum '00000000000000000000000000000000', -- md5 sum.
	}

'format' is the picture format. FFmpeg makes a distinction between format and
codec, but it is not useful for covers. The name of the format is specified by
Demlo, not by FFmpeg. Hence the 'jpeg' name, instead of 'mjpeg' as FFmpeg puts
it.

'width' and 'height' hold the size in pixels.

'checksum' can be used to identify files uniquely. For performance reasons, only
a partial checksum is performed. This variable is typically used for skipping
duplicates.

Cover transformations are specified in

	output.embeddedcovers = {
		[<cover index>] = outputcover
	}
	output.externalcovers = {
		["cover basename"] = outputcover
	}
	output.onlinecover = outputcover

'outputcover' is the following structure:

	{
		path 'full/path/with.ext',
		format '', -- e.g. 'mjpeg'.
		parameters = {},
	}

See the comment on 'format' for 'inputcover'.

'parameters' is used in the same fasion as 'output.parameters'.



Files

User configuration:
	$XDG_CONFIG_HOME/demlo/demlorc (Default: $HOME/.config/demlo/demlorc)
This must be a Lua file. See the 'demlorc' file provided with this package for
some inspiration.

Folder containing the official scripts:
	$XDG_DATA_DIRS/demlo/scripts (Default: /usr/local/share/demlo/scripts:/usr/share/demlo/scripts)

User script folder:
	$XDG_CONFIG_HOME/demlo/scripts (Default: $HOME/.config/demlo/scripts)
Create this folder and add your own scripts inside. This folder takes precedence
over the system folder, so scripts with the same name will be found in the user
folder first.


Examples

The following examples will not proceed unless the '-p' command-line option is true.

Important: you _must_ use single quotes for the runtime Lua command to prevent
expansion. Inside the Lua code, use double quotes for strings and escape single
quotes.

Show default options:
	demlo -h

Preview changes made by default scripts:
	demlo -g audio.file

Use 'alternate' script if found in user or system script folder:
	demlo -s alternate audio.file

Process the designated script file. There must be at slash or it will look up
in the user or system script folder. If the script is located in current folder,
simply prepend it with './'. This feature is convenient if you want to write
scripts that are too complex to fit on the command-line, but not generic enough
to fit the user/system script folders.
	demlo -s path/to/local/script audio.file

Run the 'case' and 'path' scripts in that specific order:
	demlo -s 'case' -s 'path' audio.file

Do not run any script but 'path', the file content is unchanged, and the file
is renamed to a dynamically computed destination:
	demlo -s 'filename' audio.file

Run default script (if set in configuration file), be do not re-encode:
	demlo -post 'output.format=input.format; output.paramters={"-c:a","copy"}' audio.file

Set 'artist' to be 'composer', and 'title' to be preceded by the new value
of 'artist', then apply default script. Do not re-encode. Order in runtime
script matters. Mind the double quotes.
	demlo -e 'o.artist=o.composer; o.title=o.artist .. " - " .. o.title' audio.file

Set track number to first number in input file name:
	demlo -pre 'o.track=input.filename:match([[.*\/\D*(\d*)\D*]])' audio.file

Apply default script but keep original value for the 'artist' tag:
	demlo -post 'o.artist=i.artist' audio.file

1) Preview default script in index format and output to 'index'. 2) Edit file to
fix any potential mistake. 3) Run Demlo over the same files using the
index information only.

	demlo *.wv >> index
	## Edit index as needed
	demlo -i index -s '' *.wv

Same as above but generate output filename according to the 'rename' script.
If you perform some manual changes after a script is run, filename is not
changed dynamically.
	demlo -i index -s rename *.wv

Retrieve tags from Internet:
	demlo -t audio.file

Same as above but for a whole album, and saving the result in an index:
	demlo -t album/*.ogg > album-index.json

Download cover for the album corresponding to the track:
	demlo -c -s 'cover' album/track

Change tags inplace with entry from MusicBrainz:
	demlo -t -s '' album/*

Set tags to titlecase while casing AC-DC correctly:
	demlo -pre 'const={"AC-DC"}' -s case audio.file

To easily switch between formats from command-line, create one script per format (see encoding.lua), e.g. ogg.lua and flac.lua. Then
	demlo -s flac -s ... audio.file
	demlo -s ogg -s ... audio.file

Add support for non-default formats from CLI:
	demlo -ext webm -pre 'output.format="webm"' audio.webm



See also

ffmpeg(1), ffprobe(1),
http://www.lua.org/pil/contents.html
*/
package main
