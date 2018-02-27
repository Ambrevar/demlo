-- demlo script
help([[
Remove source file after processing.

Demlo strives to be as non-destructive as possible.
Source files are kept untouched by default.

With this script enabled, Demlo removes the sources files if successfully
processed.

Don't use this option lightly!  The source file will be unrecoverable, which
might be problematic if you notice mistakes too late.

EXAMPLES

	demlo -r '' -s path -s 90-removesource audio.file

Do not use any script but '60-path'. The file content is unchanged and the file
is renamed to a dynamically computed destination. Demlo performs an instant
rename if destination is on the same device. Otherwise it copies the file and
removes the source.
]])

output.removesource = true
