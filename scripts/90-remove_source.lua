-- demlo script
help([[
Remove source file after processing.

Demlo strives to be as non-destructive as possible.
Source files are kept untouched by default.

With this script enabled, Demlo removes the sources files if successfully
processed.

Don't use this option lightly!  The source file will be unrecoverable, which
might be problematic if you realize mistakes too late.
]])

output.removesource = true
