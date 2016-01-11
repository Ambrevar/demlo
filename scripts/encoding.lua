-- demlo script
-- Set codec parameters. Set format (container).
-- Copy stream only when 'bitrate' is higher than 'input.bitrate'.
-- Format is kept if supported.

-- TODO: Check which format supports video streams. (E.g. for embedded covers.)

-- Global options.
-- If 'bitrate' is not specified, assume highest value. 'bitrate' should not be
-- set to 'input.bitrate', or the bitrate of the first track will propagate to
-- other tracks.
bitrate = bitrate or 9999999

-- Properties.
local AACMAX = 529000
local MP3MAX = 320000
local OGGMAX = 500000
local OPUSMAX = 512000

if output.format == '' or output.format == input.format.format_name then
	if input.format.format_name == 'ape' or input.format.format_name == 'wav' then
		-- FFmpeg does not support 'ape' encoding. 'wav' is too big.

		-- Force reencoding. Lossless format do not use the bitrate value, we
		-- decrement the bitrate just to trigger the stream encoding condition.
		bitrate = input.bitrate - 1

		output.format = 'flac'
		-- WavPack:
		-- output.format = 'wv'

	elseif input.format.format_name == 'mov,mp4,m4a,3gp,3g2,mj2' then
		-- Help ffprobe to pin down the MPEG-4 subformat.
		if i.major_brand == '3gp4' then
			output.format = '3gp'
		elseif i.major_brand == '3g2a' then
			output.format = '3g2'
		elseif i.major_brand == 'qt  ' then
			output.format = 'mov'
		else
			-- FFmpeg does not support m4a. Use mp4 instead.
			output.format = 'mp4'
		end
	end
end

local function append(t1, t2)
	for _, v in ipairs(t2) do
		t1[#t1+1] = v
	end
end

if bitrate >= input.bitrate then
	-- Encode stream only if current bitrate is strictly below the original bitrate.
	append(output.parameters, {'-c:a', 'copy'})

elseif output.format == 'adts' or
	output.format == '3gp' or
	output.format == '3g2' or
	output.format == 'mov' or
	output.format == 'mp4' then
	append(output.parameters, {'-c:a', 'aac', '-b:a', tostring(math.min(bitrate, AACMAX)), '-strict', '-2'})

elseif output.format == 'ogg' or output.format == 'oga' then
	append(output.parameters, {'-c:a', 'libvorbis', '-b:a', tostring(math.min(bitrate, OGGMAX))})
	-- Opus:
	-- append(output.parameters, {'-c:a', 'libopus', '-b:a', tostring(math.min(input.bitrate, OPUSMAX))})

elseif output.format == 'flac' then
	append(output.parameters, {'-c:a', 'flac', '-compression_level', '12'})

elseif output.format == 'wv' then
	append(output.parameters, {'-c:a', 'wavpack', '-compression_level', '12'})

elseif output.format == 'mp3' then
	if bitrate > MP3MAX then
		bitrate = MP3MAX
	end
	-- Warning: ffprobe does not return the CBR/VBR property, which is an
	-- issue if we want to turn CBR to VBR. A workaround is to set
	-- 'bitrate = input.bitrate - 1'.

	-- VBR encoding: we match current bitrate with bitrate associated to quality
	-- factor.
	local qualMap = {245000, 225000, 190000, 175000, 165000, 130000, 115000, 100000, 85000, 65000}
	local qual = 1
	for q, b in ipairs(qualMap) do
		if b < bitrate then
			break
		end
		qual = q
	end

	append(output.parameters, {'-c:a', 'libmp3lame', '-q:a', tostring(qual)})
end
