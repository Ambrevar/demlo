-- demlo script
--[[
Remove source file after processing.
Demlo strives to be as non-destructive as possible.
Source files are kept untouched unless output.removesource is true, in which
case Demlo removes the sources files if successfully processed.

Don't use this option lightly!  The source file will be unrecoverable, which
might be problematic if you realize mistakes too late.
--]]

output.removesource = true
