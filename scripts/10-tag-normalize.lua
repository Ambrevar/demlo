-- demlo script
help([[
Sanitize tags dynamically.

RULES

- Unknown tag fields are removed.

- Tags 'album_artist', 'artist', and 'composer' are easily mixed up. You may
  need to switch their values from command-line on a per-album basis.

- Note that the term "classical" refers to both western art music from 1000 AD
  to present time, and the era from 1750 to 1820. In this script the genre
  "Classical" refers to the 1750-1820 era.

EXAMPLES

	demlo -pre 'o.artist=o.composer; o.title=o.artist .. " - " .. o.title' audio.file

Set 'artist' to the value of 'composer', and 'title' to be preceded by the new
value of 'artist', then apply the default script.  Mind the double quotes.
]])


-- Start from a clean set of tags.
local tags = {}

local function empty(s)
	if type(s) ~= 'string' or s == '' then
		return true
	else
		return false
	end
end

local function first_non_empty(...)
	local args = {...}
	for _, v in pairs(args) do
		if not empty(v) then return v end
	end
end

tags.album = o.album

-- Mostly used for classical music.
tags.performer = first_non_empty(o.performer, o.conductor, o.orchestra, o.arranger)

tags.artist = first_non_empty(o.artist, o.composer, o.album_artist, tags.performer, 'Unknown Artist')

tags.album_artist = first_non_empty(o.album_artist, tags.artist)
-- If 'album_artist == "various artists"' (e.g. for compilations) then we use
-- 'album' as 'album_artist' since it is more meaningful.
if stringrel(stringnorm (o.album_artist), 'variousartist') > 0.7 then
	tags.album_artist = o.album
end

tags.title = empty(o.title) and 'Unknown Title' or o.title

help([[
- Genre: since this is not universal by nature, we avoid setting a genre in
  tags, except for special cases like soundtracks and classical music. We
  analyse the input genre and make sure it fits an era. This is sometimes
  ambiguous. You may be better off leaving it empty. We convert to lowercase
  and spaces to underscores to ease matching.
]])
tags.genre = o.genre
local relmax = 0
local genre_classical = {
	'Medieval',
	'Renaissance',
	'Baroque',
	'Classical',
	'Romantic',
	'Modern',
	'Contemporary',
}
local genre_others = {
	'Soundtrack',
	'Humour'
}
local genre = tags.genre
for _, g in pairs(genre_classical) do
	local rel = stringrel(stringnorm(g), stringnorm (tags.genre))
	if rel > relmax then
		relmax = rel
		genre = g
	end
end
for _, g in pairs(genre_others) do
	local rel = stringrel(stringnorm(g), stringnorm (tags.genre))
	if rel > relmax then
		relmax = rel
		genre = g
		-- We only use performer for classical music.
		tags.performer = nil
	end
end
if relmax < 0.7 then
	tags.genre = nil
	-- We only use performer for classical music.
	tags.performer = nil
else
	tags.genre = genre
end

help([[
- Disc and track numbers only matter if the file is part of an album. Remove
  the leading zeros and consider the first number only (e.g. convert "01/17" to
  "1".
]])
tags.disc = not empty(o.album) and not empty(o.disc) and o.disc:match([[0*(\d*)]]) or nil
tags.track = not empty(o.album) and not empty(o.track) and o.track:match([[0*(\d*)]]) or nil

help([[
- Date: Only use the full year. Extract from the date the first number
  with 4 digits or more.
]])
tags.date = o.date and o.date:match([[\d\d\d\d+]]) or nil
tags.date = tags.date and tags.date or (o.year and o.year:match([[\d\d\d\d+]]) or '')

-- Replace all tags.
output.tags = tags
o = output.tags


help([[
REFERENCES

- http://musicbrainz.org/doc/MusicBrainz_Picard/Tags/Mapping
- http://musicbrainz.org/doc/Classical_Music_FAQ
]])
