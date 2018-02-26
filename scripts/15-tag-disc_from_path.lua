-- demlo script
help([[
Get disc numberfrom the single digit of the parent folder.
If there is none, remove disc number.
]])

local parent = input.path:match("(.*)/")
local dirname = parent and parent:match(".*/(.*)")

o.disc = dirname and (
	dirname:match([[\D(\d)\D]])
	or dirname:match([[\D(\d)$]])
	or dirname:match([[^(\d)\D]])
	or dirname:match([[^\d$]])
)
