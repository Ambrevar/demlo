-- Proceed for audio files only.
if existinfo.trackcount ~= 0 then
	-- Overwrite if 'input' is more recent than destination, skip otherwise.
	if input.time.sec > existinfo.time.sec or
		(input.time.sec == existinfo.time.sec and input.time.nsec > existinfo.time.nsec) then
		output.write = 'overwrite'
	else
		output.write = 'skip'
	end
end
