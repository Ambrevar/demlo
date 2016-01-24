-- demlo script
-- Set the output path according to tags.

-- Make sure no unnecessary subfolders are created.
-- Extension is set from format.
-- Pad zeros (2 digits) in track number for file browsers without numeric sorting capabilities.

-- Global options.
local osseparator = ossep or '/'
-- Relative paths are OK, but '~' does not get expanded.
local library = lib or os.getenv('HOME')  .. osseparator .. 'music'
-- Replace OS separators in folder names by ' - ' in the sane() function.
-- Some filesystems require that more characters get replaced, such as ':'.
local fsfilter = fsf or [[\s*]] .. osseparator .. [[\s*]]

local function empty(s)
	if type(s) ~= 'string' or s == '' then
		return true
	else
		return false
	end
end

local function sane(s)
	return s:gsub(fsfilter, ' - ')
end

local function append(field, before, after)
	if not empty(field) then
		before = before or ''
		after = after or ''
		output.path = output.path .. sane(before) .. sane(field) .. sane(after)
	end
end

local track_padded = ''
if not empty (o.track) and tonumber(o.track) then
	track_padded = string.format('%02d', o.track)
end

-- We try to guess if the genre belongs to classical music.
local genre = o.genre and o.genre:lower():gsub([[\s]],'_')
local classical = false
if genre then
	local genre_conv = {
		medieval = 'Medieval',
		renaissance = 'Renaissance',
		baro = 'Renaissance',
		classic = 'Classical',
		romantic = 'Romantic',
		modern = 'Modern',
		contemp = 'Contemporary',
	}
	for search, _ in pairs(genre_conv) do
		if genre:match(search) then
			classical = true
			break
		end
	end
end

-- Output path.
output.path = library .. osseparator
local album_artist = not empty(o.album_artist) and o.album_artist or
	(not empty(o.artist) and o.artist or 'Unknown Artist')
append(album_artist)
output.path = output.path .. osseparator

if not empty(o.album) then
	if classical then
		-- Since classical pieces usually get recorded several times, the date is
		-- not very relevant. Thus it is preferable to sort albums by name on the
		-- filesystem.
		append(o.album)
		append(o.performer, ' (', (not empty(o.date) and (', ' .. sane(o.date)) or '') .. ')')
	else
		append(o.date, nil, '. ')
		append(o.album)
	end
	append(o.disc, ' - Disc ')
	output.path = output.path .. osseparator
	append(track_padded, nil, '. ')
end

if o.artist ~= o.album_artist then
	append(o.artist, nil, ' - ')
end
append(o.title)

local ext = empty(output.format) and input.format.format_name or output.format
append('.' .. ext)
