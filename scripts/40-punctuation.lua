-- demlo script
help([[
Fix punctuation.

It is hard to guess the language, thus we fall back to the English rules: No
space before the mark, one space after it.
]])

local function fix_punctuation(input)
	-- Convert underscore to space. Do this first.
	input = input:gsub('_', ' ')

	-- Add a space after some closing marks. '.' can be used for initials, ':' for
	-- dates, ',' as a digit separator: don't append a space after them.
	input = input:gsub([[([;!?)\]}])]], '$1 ')

	-- Remove spacing before closing marks.
	input = input:gsub([[\s+([.,:;!?)\]}]+)]], '$1')

	-- Add a space before opening marks.
	input = input:gsub([[([([{])]], ' $1')

	-- Remove spacing after opening marks.
	input = input:gsub([[([([{])\s+]], '$1')

	-- Convert spacing to one single space.
	input = input:gsub([[\s+]], ' ')

	-- Trim prefix and suffix space. Do this last.
	input = input:gsub([[^\s+]], '')
	input = input:gsub([[\s+$]], '')

	return input
end

for k, v in pairs(output.tags) do
	output.tags[k] = fix_punctuation(v)
end
