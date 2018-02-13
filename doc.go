// Copyright © 2013-2018 Pierre Neidhardt <ambrevar@gmail.com>
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
the list. Identical files are appended only once.

Next all files get analyzed:

- The audio file details (tags, stream properties, format properties, etc.) are
stored into the 'input' variable. The 'output' variable gets its default values
from 'input', or from an index file if specified from command-line. If no index
has been specified and if an attached cuesheet is found, all cuesheet details
are appended accordingly. Cuesheet tags override stream tags, which override
format tags. Finally, still without index, tags can be retrieved from Internet
if the command-line option is set.

- If a prescript has been specified, it gets executed. It makes it possible to
adjust the input values and global variables before running the other scripts.

- The scripts, if any, get executed in the lexicographic order of their
basename. The 'output' variable is transformed accordingly. Scripts may contain
rules such as defining a new file name, new tags, new encoding properties, etc.
You can use conditions on input values to set the output properties, which makes
it virtually possible to process a full music library in one single run.

- If a postscript has been specified, it gets executed. It makes it possible to
adjust the output of the script for the current run only.

- Demlo makes some last-minute tweaking if need be: it adjusts the bitrate, the
path, the encoding parameters, and so on.

- A preview of changes is displayed.

- When applying changes, the covers get copied if required and the audio file
gets processed: tags are modified as specified, the file is re-encoded if
required, and the output is written to the appropriate folder. When destination
already exists, the 'exist' action is executed.



Configuration

The program's default behaviour can be changed from the user configuration file.
(See the 'Files' section for a template.) Most command-line flags default value
can be changed. The configuration file is loaded on startup, before parsing the
command-line options. Review the default value of the CLI flags with 'demlo -h'.

If you wish to use no configuration file, set the environment variable
DEMLO_CONFIG to ".".



Scripts

Scripts can contain any safe Lua code. Some functions like 'os.execute' are not
available for security reasons. It is not possible to print to the standard
output/error unless running in debug mode and using the 'debug' function.

See the 'sandbox.go' file for a list of allowed functions and variables.

Lua patterns are replaced by Go regexps. See
https://github.com/google/re2/wiki/Syntax.

Scripts have no requirements at all. However, to be useful, they should set
values of the 'output' table detailed in the 'Variables' section. You can use
the full power of the Lua to set the variables dynamically. For instance:

	output.path = library .. '/' .. o.artist .. '/' .. (o.album ~= nil and o.album .. '/' or '') .. track_padded .. '. ' .. o.title

'input' and 'output' are both accessible from any script.

All default functions and variables (excluding 'output') are reset on every
script call to enforce consistency. Local variables are lost from one script
call to another. Global variables are preserved. Use this feature to pass data
like options or new functions.

'output' structure consistency is guaranteed at the start of every script. Demlo
will only extract the fields with the right type as described in the 'Variables'
section.

Warning: Do not abuse of global variables, especially when processing non-fixed
size data (e.g. tables). Data could grow big and slow down the program.



Existing destination

By default, when the destination exists, Demlo will append a suffix to the
output destination. This behaviour can be changed from the 'exist' action
specified by the user. Demlo comes with a few default actions.

The 'exist' action works just like scripts with the following differences:
- Any change to 'output.path' will be skipped.
- An additional variable is accessible from the action: 'existinfo' holds the file
details of the existing files in the same fashion as 'input'. This allows for
comparing the input file and the existing destination.

The writing rules can be tweaked the following way:

	output.write = 'skip' // Skip current file.
	output.write = 'overwrite' // Overwrite existing destination.
	output.write = '' // Anything else: append random suffix (default)

Word of caution: overwriting breaks Demlo's rule of not altering existing files.
It can lead to undesired results if the overwritten file is also part of the
(yet to be processed) input. The overwrite capability can be useful when syncing
music libraries however.



Runtime code

The user scripts should be generic. Therefore they may not properly handle some
uncommon input values. Tweak the input with temporary overrides from
command-line.

The prescript and postscript defined on command-line will let you run arbitrary
code that is run before and after all other scripts, respectively.

Use global variables to transfer data and parameters along.

If the prescript and postscript end up being too long, consider writing a demlo
script. You can also define shell aliases or use wrapper scripts as convenience.



Variables

The 'input' table describes the file:

	input = {
	   path = '/real/path',
	   bitrate = 0,
	   tags = {},
	   time = {
	      sec = 0,
	      nsec = 0,
	   }
	   audioindex = 0,
	   streams = {},
	   format = {},
	   embeddedcovers = {},
	   externalcovers = {},
	   onlinecover = {},
	}

Bitrate is in bits per seconds (bps). That is, for 320 kbps you would specify
	output.bitrate = 320000

The 'time' is the modification time of the file. It holds the sec seconds and
nsec nanoseconds since January 1, 1970 UTC.

The entry 'streams' and 'format' are as returned by
	ffprobe -v quiet -print_format json -show_streams -show_format $file
It gives access to most metadata that FFmpeg can return. For instance, to get
the duration of the track in seconds, query the variable
'input.format.duration'.

Since there may be more than one stream (covers, other data), the first audio
stream is assumed to be the music stream. For convenience, the index of the
music stream is stored in 'audioindex'.

The tags returned by FFmpeg are found in streams, format and in the cuesheet.
To make tag queries easier, all tags are stored in the 'tags' table, with the
following precedence:

	format tags < stream tags < cuesheet header tags < cuesheet track tags

You can remove a tag by setting it to 'nil' or the empty string. This is
equivalent, except that 'nil' saves some memory during the process.


The 'output' table describes the transformation to apply to the file:

	output = {
	   path = 'full/path/with.ext',
	   format = 'format',
	   parameters = {},
	   tags = {},
	   embeddedcovers = {},
	   externalcovers = {},
	   onlinecover = {},
	   write = '',
	   rmsrc = false,
	}

The 'parameters' array holds the CLI parameters passed to FFmpeg. It can be
anything supported by FFmpeg, although this variable is supposed to hold
encoding information. See the 'Examples' section.

The 'embeddedcovers', 'externalcovers' and 'onlinecover' variables are detailed
in the 'Covers' section.

The 'write' variable is covered in the 'Existing destination' section.

The 'rmsrc' variable is a boolean: when true, Demlo removes the source file
after processing. This can speed up the process when not re-encoding. This
option is ignored for multi-track files.

For convenience, the following shortcuts are provided:
	i = input.tags
	o = output.tags



Functions

Demlo provides some non-standard Lua functions to ease scripting.

	debug(string...)
Display a message on stderr if debug mode is on.

	stringnorm(string)
Return lowercase string without non-alphanumeric characters nor leading zeros.

	stringrel(string, string)
Return the relation coefficient of the two input strings. The result is a float
in 0.0...1.0, 0.0 means no relation at all, 1.0 means identical strings.



Encoding

A format is a container in FFmpeg's terminology.

'output.parameters' contains CLI flags passed to FFmpeg. They are meant to set
the stream codec, the bitrate, etc.

If 'output.parameters' is {'-c:a', 'copy'} and the format is identical, then
taglib will be used instead of FFmpeg. Use this rule from a (post)script to
disable encoding by setting the same format and the copy parameters. This speeds
up the process.



Preview

The official scripts are usually very smart at guessing the right values. They
might make mistakes however. If you are unsure, you can (and you are advised to)
preview the results before proceeding. The 'diff' preview is printed to stderr.
A JSON preview of the changes is printed to stdout if stdout is redirected.


Internet service

The initial values of the 'output' table can be completed with tags fetched from
the MusicBrainz database. Audio files are fingerprinted for the queries, so even
with initially wrong file names and tags, the right values should still be
retrieved. The front album cover can also be retrieved.

Proxy parameters will be fetched automatically from the 'http_proxy'
and 'https_proxy' environment variables.

As this process requires network access it can be quite slow. Nevertheless,
Demlo is specifically optimized for albums, so that network queries are
used for only one track per album, when possible.

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
the output to a file, edit the content manually with your favorite text editor,
then run Demlo again with the index as argument. See the 'Examples' section.

This feature can also be used to interface Demlo with other programs.



Covers

Demlo can manage embedded covers as well as external covers.

External covers are queried from files matching known extensions in the file's
folder.
Embedded covers are queried from static video streams in the file.
Covers are accessed from

	input.embeddedcovers = {
		[<cover index>] = inputcover
	}
	input.externalcovers = {
		['cover basename'] = inputcover
	}
	input.onlinecover = inputcover

The embedded covers are indexed numerically by order of appearance in the
streams. The first cover will be at index 1 and so on. This is not necessarily
the index of the stream.

'inputcover' is the following structure:

	{
		format = '', -- (Can be any of 'gif', 'jpeg', 'png'.)
		width = 0,
		height = 0,
		checksum = '00000000000000000000000000000000', -- md5 sum.
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
		['cover basename'] = outputcover
	}
	output.onlinecover = outputcover

'outputcover' has the following structure:

	{
		path = 'full/path/with.ext',
		format = '', -- e.g. 'mjpeg', 'png'.
		parameters = {},
	}

The format is specified by FFmpeg this time. See the comments on 'format' for
'inputcover'.

'parameters' is used in the same fashion as 'output.parameters'.



Files

User configuration:
	$XDG_CONFIG_HOME/demlo/config.lua (Default: $HOME/.config/demlo/config.lua)
This must be a Lua file. See the 'config.lua' file provided with this package for
an exhaustive list of options.

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

Preview changes made by the default scripts:
	demlo audio.file

Use 'alternate' script if found in user or system script folder (user folder first):
	demlo -s alternate audio.file

Add the Lua file to the list of scripts. This feature is convenient if you want
to write scripts that are too complex to fit on the command-line, but not
generic enough to fit the user or system script folders.
	demlo -s path/to/local/script.lua audio.file

Remove all script from the list, then add '30-case' and '60-path' scripts. Note
that '30-case' will be run before '60-path'.
	demlo -r '' -s 60-path -s 30-case audio.file

Do not use any script but '60-path'. The file content is unchanged and the file
is renamed to a dynamically computed destination. Demlo performs an instant
rename if destination is on the same device. Otherwise it copies the file and
removes the source.
	demlo -r '' -s 60-path -s 90-rmsrc audio.file

Use the default scripts (if set in configuration file), but do not re-encode:
	demlo -post 'output.format=input.format; output.parameters={"-c:a","copy"}' audio.file

Set 'artist' to the value of 'composer', and 'title' to be preceded by the new
value of 'artist', then apply the default script. Do not re-encode. Order in
runtime script matters. Mind the double quotes.
	demlo -e 'o.artist=o.composer; o.title=o.artist .. " - " .. o.title' audio.file

Set track number to first number in input file name:
	demlo -pre 'o.track=input.path:match([[.*\/\D*(\d*)\D*]])' audio.file

Use the default scripts but keep original value for the 'artist' tag:
	demlo -post 'o.artist=i.artist' audio.file

1) Preview default scripts transformation and save it to an index. 2) Edit file
to fix any potential mistake. 3) Run Demlo over the same files using the index
information only.

	demlo *.wv >> index
	## Oops! Forgot some files:
	demlo *.flac >> index
	## Edit index as needed...
	demlo -p -i index -r '' *.wv

Same as above but generate output filename according to the custom '61-rename'
script. The numeric prefix is important: it ensures that '61-rename' will be run
after all the default tag related scripts and after '60-path'. Otherwise, if a
change in tags would occur later on, it would not affect the renaming script.
	demlo -i index -s 61-rename *.wv

Retrieve tags from Internet:
	demlo -t audio.file

Same as above but for a whole album, and saving the result to an index:
	demlo -t album/*.ogg > album-index.json

Only download the cover for the album corresponding to the track. Use 'rmsrc' to
avoid duplicating the audio file.
	demlo -c -r "" -s 70-cover -s 90-rmsrc album/track

Change tags inplace with entries from MusicBrainz:
	demlo -t -r '' album/*

Set tags to titlecase while casing AC-DC correctly:
	demlo -pre 'const={"AC-DC"}' -s 30-case audio.file

To easily switch between formats from command-line, create one script per format
(see 50-encoding.lua), e.g. ogg.lua and flac.lua. Then
	demlo -s flac -s ... audio.file
	demlo -s ogg -s ... audio.file

Add support for non-default formats from CLI:
	demlo -ext webm -pre 'output.format="webm"' audio.webm

Overwrite existing destination if input is newer:
	demlo -exist writenewer audio.file


See also

ffmpeg(1), ffprobe(1),
http://www.lua.org/pil/contents.html
*/
package main
