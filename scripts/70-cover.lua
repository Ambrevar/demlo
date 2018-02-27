-- demlo script
help([[
Process album covers / artwork.

- Remove embedded covers.
- Convert to JPEG.
- Skip covers beyond quality threshold.
- Skip duplicates.

VARIABLES

Demlo can manage embedded covers as well as external covers.

Demlo skips covers with no output path. It copies covers with no codec or no
format. It transcodes covers when the codec or the format are different from the
default.

External covers are queried from files matching known extensions in the file's
folder.
Embedded covers are queried from static video streams in the file.
Covers are accessed from

	input.embeddedcovers = {
		[#cover index] = inputcover -- See below for "inputcover".
	}
	input.externalcovers = {
		['cover basename'] = inputcover
	}
	input.onlinecover = inputcover

The embedded covers are indexed numerically by order of appearance in the
streams. The first cover will be at index 1 and so on. This is not necessarily
the index of the stream.

'inputcover' has the following structure:

	{
		format = '', -- (Can be any of 'gif', 'jpeg', 'png'.)
		width = 0,
		height = 0,
		checksum = '00000000000000000000000000000000', -- md5 sum.
	}

'format' is the image format. FFmpeg makes a distinction between format and
codec, but it is not useful for covers.  For images, input format names are
different from the output formats which use the Go nomenclature. Default output
formats are 'gif', 'jpeg' and 'png'.  The name of the format is specified by
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

EXAMPLES

	demlo -c -r '' -s cover -s remove_source album/track

Only download the cover for the album corresponding to the track. We use
'removesource' to avoid duplicating the audio file.
]])

-- Even though FFmpeg makes a distinction between format (container) and codec,
-- this is not useful for covers.

-- Properties
local LIMIT_LOW = 128
local LIMIT_HIGH = 1024

local dirname = output.path:match ('^(.*)/') or '.'
local basename = 'Cover'
if output.tags.album then
	basename = output.tags.album .. ' - Cover'
end

local checksum_list = {}

local function to_jpeg(input_cover, stream, file)
	stream = stream or 0

	local id = file and tostring(file) or stream and 'stream ' .. tostring(stream) or 'online'

	local output_cover = {}
	output_cover.parameters = {}

	if input_cover.width < LIMIT_LOW or input_cover.height < LIMIT_LOW then
		debug('Script cover: skip low quality: ' .. id  ..  ' ([' .. tostring(input_cover.width) .. 'x' .. tostring(input_cover.height) .. '] < [' .. tostring(LIMIT_LOW) .. 'x'  .. tostring(LIMIT_LOW) .. '])')
		return output_cover
	end

	if checksum_list[input_cover.checksum] then
		debug('Script cover: skip duplicate: ' .. id ..  ' (checksum('.. checksum_list[input_cover.checksum] ..')=' .. input_cover.checksum  ..')')
		return output_cover
	end

	-- Skip future duplicates.
	checksum_list[input_cover.checksum] = id

	local max_ratio = math.max(input_cover.width / LIMIT_HIGH, input_cover.height / LIMIT_HIGH)
	if max_ratio > 1 then
		debug('Script cover: down-scale: ' .. id ..  ' ([' .. tostring(input_cover.width) .. 'x' .. tostring(input_cover.height) .. '] > [' .. tostring(LIMIT_HIGH) .. 'x'  .. tostring(LIMIT_HIGH) .. '])')
		output_cover.parameters[#output_cover.parameters+1] = '-s'
		output_cover.parameters[#output_cover.parameters+1] = math.floor(input_cover.width/max_ratio + 0.5) .. 'x' .. math.floor(input_cover.height/max_ratio + 0.5)

	elseif input_cover.format == 'jpeg' then
		-- Already in JPEG, do not convert.
		output_cover.parameters = nil
	else
		-- Convert to JPEG.
		output_cover.parameters[#output_cover.parameters+1] = '-c:'.. stream
		output_cover.parameters[#output_cover.parameters+1] = 'mjpeg'
	end

	output_cover.format = 'mjpeg'
	output_cover.path = dirname .. '/' .. basename .. '.jpg'

	return output_cover
end

for stream, input_cover in pairs(input.embeddedcovers) do
	-- Extract embedded covers.
	output.embeddedcovers[stream] = to_jpeg(input_cover, stream)

	-- Remove all embedded covers.
	output.parameters[#output.parameters+1] = '-vn'
end

for file, input_cover in pairs(input.externalcovers) do
	output.externalcovers[file] = to_jpeg(input_cover, nil, file)
end

if input.onlinecover.format ~= "" then
	output.onlinecover = to_jpeg(input.onlinecover)
end
