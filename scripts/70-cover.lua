-- demlo script
-- Remove embedded covers. Convert to jpeg. Skip covers beyond quality threshold. Skip duplicates.

-- Even though FFmpeg makes a distinction between format (container) and codec,
-- this is not useful for covers.

-- Input format names are different from the output formats which use Go
-- nomenclature. Default output formats are 'gif', 'jpeg' and 'png'.

-- Demlo skips covers with no path. It copies covers with no parameters or no
-- format. It transcodes covers with non-default parameters and format.

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
		-- Already in jpeg, do not convert.
		output_cover.parameters = nil
	else
		-- Convert to jpeg.
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
