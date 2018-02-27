// Copyright © 2013-2018 Pierre Neidhardt <ambrevar@gmail.com>
// Use of this file is governed by the license that can be found in LICENSE.

package main

var help = `
Demlo is a music library organizer. It can encode, fix case, change path
according to tags or file properties, tag from an online database, copy covers
while ignoring duplicates or those below a quality threshold, and much more. It
makes it possible to manage your libraries uniformly and dynamically. You can
write your own rules in Lua scripts to best fit your needs.



PROCESS

First Demlo creates a list of all input files. When a folder is specified, all
files matching the extensions from the 'extensions' variable will be appended to
the list. Identical files are appended only once.

Next all files get analyzed:

- The audio file details (tags, stream properties, format properties, etc.) are
stored into the 'input' variable. The 'output' variable gets its default values
from 'input', or from an index file if specified from command-line (see INDEX
section). If no index has been specified and if an attached cuesheet is found,
all cuesheet details are appended accordingly. Cuesheet tags override stream
tags, which override format tags. Finally, still without index, tags can be
retrieved from Internet if the command-line option is set.

- If a 'prescript' has been specified, it gets executed. It makes it possible to
adjust the input values and global variables before running the other scripts.

- The scripts, if any, get executed in the lexicographic order of their
basename. The 'output' variable is transformed accordingly (see VARIABLES
section). Scripts may contain rules such as defining a new file name, new tags,
new encoding properties, etc.  You can use conditions on input values to set the
output properties, which makes it virtually possible to process a full music
library in one single run.

- If a 'postscript' has been specified, it gets executed. It allows to adjust
the output of the scripts from the commandline.

- Demlo makes some last-minute tweaking if need be: it adjusts the bitrate, the
path, the encoding parameters, and so on.

- A preview of changes is displayed.

- When applying changes, the covers get copied if required and the audio file
gets processed: tags are modified as specified, the file is re-encoded if
required, and the output is written to the appropriate folder. When destination
already exists, the 'exist' action is executed (see EXISTING DESTINATION
section).



CONFIGURATION

The program's default behaviour can be changed from the a configuration file.
The user configuration is found in

	$XDG_CONFIG_HOME/demlo/config.lua (Default: $HOME/.config/demlo/config.lua)

If not found, then the system configuration is used:

	$XDG_DATA_DIRS/demlo/config.lua (Default: /usr/local/share/demlo/config.lua or
                                            /usr/share/demlo/config.lua)

The system configuration can provide a good starting point for the user
configuration.  Most commandline flags default value can be changed. The
configuration file is loaded on startup, before parsing the commandline
options. You can review the default value of the commandline flags with by
starting 'demlo' without argument.

If you wish to use no configuration file, set the environment variable
DEMLO_CONFIG to ".".



SCRIPTS

Scripts can contain any sandboxed Lua code.  They have no requirements at
all. To be useful however, they should set values of the 'output' table detailed
in the VARIABLES section. You can use idiomatic Lua to set the variables
dynamically. For instance:

  output.path = library .. '/' ..
    o.artist .. '/' ..
    (o.album ~= nil and o.album .. '/' or '') ..
    track_padded .. '. ' ..
    o.title

'input' and 'output' are both accessible from any script.

All default functions and variables except 'output' are reset on every script
call to enforce consistency. Local variables are lost from one script call to
another. Global variables are preserved. You can use this feature to pass data
between scripts, for instance options or new functions.

The 'output' structure consistency is guaranteed at the start of every
script. Demlo will only extract the fields with the right type as described in
the 'Variables' section.

Warning: Do not abuse of global variables, especially when processing non-fixed
size data (e.g. tables). Data could grow big and slow down the program.

Some functions like 'os.execute' are not available to prevent scripts from
altering the system. It is not possible to print to the standard output/error
unless running in debug mode and using the 'debug' function.

See the 'sandbox.go' source file for a list of allowed functions and variables.

Lua patterns are replaced by Go regexes. See
https://github.com/google/re2/wiki/Syntax.

The official scripts are stored in

	$XDG_DATA_DIRS/demlo/scripts (Default: /usr/local/share/demlo/scripts or
                                         /usr/share/demlo/scripts)

The user script folder is located at

	$XDG_CONFIG_HOME/demlo/scripts (Default: $HOME/.config/demlo/scripts)

The user script folder might have to be created before you can add your own
scripts inside. The user folder takes precedence over the system folder, thus
scripts with the same basename will be found in the user folder.



RUNTIME CODE (PRESCRIPT & POSTSCRIPT)

The user scripts are most useful when they are generic enough to be applied on
any file.  Therefore they may not properly handle some uncommon input
values. You can tweak the input with temporary overrides from commandline thanks
to the 'prescript' and the 'postscript'.  They will let you run sandboxed Lua
code before and after all other scripts, respectively.

Use global variables to transfer data and parameters along.

If the prescript and postscript end up being too long, consider writing a Demlo
script. You can also define shell aliases or use wrapper scripts as convenience.



EXISTING DESTINATION

By default, when the destination exists, Demlo will append a suffix to the
output destination. This behaviour can be changed from the 'exist' action
specified by the user. Demlo comes with a few default actions.

The 'exist' action works just like scripts with the following differences:
- Any change to 'output.path' will be skipped.
- An additional variable is accessible from the action: 'existinfo' holds the file
details of the existing file in the same fashion as 'input'. This allows for
comparing the input file and the existing destination.

The writing rules can be tweaked the following way:

	output.write = 'skip'       Skip current file.
	output.write = 'overwrite'  Overwrite existing destination.
	output.write = ''           Anything else: append random suffix (default)

Word of caution: overwriting breaks Demlo's rule of not altering existing files.
It can lead to undesired results if the overwritten file is also part of the
(yet to be processed) input. The overwrite capability comes in handy when
syncing music libraries.



VARIABLES (INPUT & OUTPUT)

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

Bitrate is in bits-per-seconds (bps). That is, for 320 kbps you would specify

	output.bitrate = 320000

The 'time' is the modification time of the file. It holds the sec seconds and
nsec nanoseconds since January 1, 1970 UTC.

The entry 'streams' and 'format' are as returned by

	$ ffprobe -v quiet -print_format json -show_streams -show_format FILE

They give access to most metadata that FFmpeg can return. For instance the
duration of the track in seconds can be found in 'input.format.duration'.

Since there may be more than one stream (covers, other data), the first audio
stream is assumed to be the music stream. For convenience, the index of the
music stream is stored in 'audioindex'.

The tags returned by FFmpeg are found in streams, format and in the cuesheet.
To make tag queries easier, all tags are stored in the 'tags' table, with the
following precedence:

	format tags < stream tags < cuesheet header tags < cuesheet track tags

You can remove a tag by setting it to 'nil' or the empty string.

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
	   removesource = false,
	}

The 'parameters' array holds the commandline parameters passed to FFmpeg. It can
be anything supported by FFmpeg, although this variable is supposed to hold
encoding information. See the EXAMPLES section.

The 'embeddedcovers', 'externalcovers' and 'onlinecover' variables are detailed
in the 'Covers' section.

The 'write' variable is explained in the EXISTING DESTINATION section.

The 'removesource' variable is a boolean: when true, Demlo removes the source file
after processing. This can speed up the process when not re-encoding. This
option is ignored for multi-track files.

For convenience, the following shortcuts are provided:

	i = input.tags
	o = output.tags



LUA FUNCTIONS

Demlo provides some non-standard Lua functions to ease scripting.

	debug(string...)
Display a message on stderr if debug mode is on.

	stringnorm(string)
Return lowercase string without non-alphanumeric characters nor leading zeros.

	stringrel(string, string)
Return the relation coefficient of the two input strings. The result is a float
in the [0.0, 1.0] range. 0.0 means no relation at all, 1.0 means identical
strings.



PREVIEW

The official scripts are usually very smart at guessing the right values. They
might make mistakes however. If you are unsure, you can (and you are advised to)
preview the before-after changes before proceeding.  A JSON preview of the
changes is printed to stdout with the '-o' commandline flag or if stdout is
redirected.



INTERNET TAGGING AND COVER FETCHING

The initial values of the 'output' table can be completed with tags fetched from
the MusicBrainz database. Audio files are fingerprinted for the queries, so even
with initially wrong file names and tags, the right values should still be
retrieved. The front album cover can also be retrieved.

Proxy parameters will be fetched automatically from the 'http_proxy'
and 'https_proxy' environment variables.

As this process requires network access it can be quite slow. Nevertheless,
Demlo is specifically optimized for albums, such that network queries are
used for only one track per album, when possible.



INDEX

Demlo can preset the 'output' variables according to the values set in a text file
before calling the scripts.

This 'index' is a JSON file and can be generated with the '-o' commandline flag
or with shell redirection if you shell supports that.  It is valid JSON except
for the missing beginning and the missing end: This makes it possible to
concatenate and to append to existing index files. Demlo will automatically
complete the missing parts so that it becomes valid JSON.

Online tagging is automatically disabled when an index is used.

The index file is useful when you want to edit tags manually: You can redirect
the output to a file, edit the content manually with your favorite text editor,
then run Demlo again with the index as argument. See the EXAMPLES section.

This feature can also be used to interface Demlo with other programs.



EXAMPLES

The following examples will not proceed unless the '-p' command-line option is
true.

Important: on most shells, you _must_ use single quotes for the runtime Lua
command to prevent shell expansion. Inside the Lua code, use double quotes for
strings and escape single quotes.

	demlo -s alternate audio.file

Add 'alternate' script to the script chain and preview the changes.  The
specified name does not contain any folder separator, thus it is found in the
user or system script folder.

	demlo -s path/to/local/script.lua audio.file

Add the local Lua file to the script chain. This feature is convenient if you
want to write scripts that are too complex to fit on the command-line, but not
generic enough to fit the user or system script folders.

	demlo -r '' -s path -s case audio.file

Remove all script from the list, then add '30-tag-case' and '60-path'
scripts. Note that '30-case' is run before '60-path'.

	demlo -post 'o.artist=i.artist' audio.file

Use the default scripts but keep original value for the 'artist' tag:

	demlo *.wv >> index
	# Oops! Forgot some files:
	demlo *.flac >> index
	# Edit index as needed...
	demlo -p -i index -r '' *.wv

1) Preview default scripts transformation and save it to an index. 2) Edit file
to fix any potential mistake. 3) Run Demlo over the same files using the index
information only.

	demlo -i index -s rename *.wv

Same as above but generate output filename according to the custom '61-rename'
script. The numeric prefix is important: it ensures that '61-rename' will be run
after all the default tag related scripts and after '60-path'. Otherwise, if a
change in tags would occur later on, it would not affect the renaming script.

	demlo -t album/*.ogg > album-index.json

Retrieve all tags from the Internet and save the result to an index.

	demlo -t -r path -s remove_source album/*

Change tags in-place with entries from MusicBrainz.

	demlo -ext webm -pre 'output.format="webm"' audio.webm

Add support for non-default formats from commandline.

	demlo -exist writenewer audio.file

Overwrite existing destination if input is newer:



SEE ALSO

- The ffmpeg(1) and ffprobe(1) man pages.
- The official Lua manual: http://www.lua.org/pil/contents.html.
`
