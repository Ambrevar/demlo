-- demlo script
-- Set case in tags either to title case or sentence case.

-- See https://en.wikipedia.org/wiki/Letter_case.

-- Global options.
local sentencecase = scase or false
local const_custom = const or {}

-- TODO: No verb? (am, are, was, is) No word > 3 chars? (against, between, from, into, onto)
const_en = const_en or {
	'a',
	'an',
	'and',
	'as',
	'at',
	'but',
	'by',
	'for',
	'if',
	'in',
	'nor',
	'not',
	'of',
	'on',
	'so',
	'the',
	'to',
	'via',
}

const_music = const_music or {
	'CD',
	'CD1',
	'CD2',
	'CD3',
	'CD4',
	'CD5',
	'CD6',
	'CD7',
	'CD8',
	'CD9',
	'DJ',
	'EP',
	'feat',
	'FX',
}

const_common = const_common or {
	'KO',
	'OK',
	'TV',
	'vs',
}

-- Some common units.
const_units = const_units or {
	'bps',
	'Gbps',
	'GHz',
	'h', -- hour
	'Hz',
	'kbps',
	'kg',
	'kHz',
	'km',
	'kph',
	'Mbps',
	'MHz',
	'ms',
	's', -- second
}

-- Word starting in Mac[X] where [X] is a consonant. See http://visca.com/regexdict/.
-- The following words are capitalized normally as opposed to MacDonalds and McCarthy.
const_mac = const_mac or {
	'Mache',
	'Machete',
	'Machicolate',
	'Machicolation',
	'Machinate',
	'Machination',
	'Machine',
	'Machinery',
	'Machinist',
	'Machismo',
	'Macho',
	'Machzor',
	'Mackerel',
	'Mackinaw',
	'Mackintosh',
	'Mackle',
	'Macle',
	'Macrame',
	'Macro',
	'Macrobiotics',
	'Macrocephaly',
	'Macroclimate',
	'Macrocode',
	'Macrocosm',
	'Macrocyte',
	'Macrocytosis',
	'Macroeconomics',
	'Macroevolution',
	'Macrofossil',
	'Macrogamete',
	'Macroglobulin',
	'Macroglobulinemia',
	'Macrograph',
	'Macrography',
	'Macroinstruction',
	'Macromere',
	'Macromolecule',
	'Macron',
	'Macronucleus',
	'Macronutrient',
	'Macrophage',
	'Macrophysics',
	'Macrophyte',
	'Macropterous',
	'Macroscopic',
	'Macrosporangium',
	'Macrospore',
}

-- Return the list of the plurals of the input.
local function pluralize(word_list)
	local result = {}
	for _, word in ipairs(word_list) do
		local lastchar = word:sub(-1)
		local plural
		if lastchar == 'y' then
			plural = word:sub(1, -2) .. "ies"
		elseif lastchar == 's' then
			plural = word
		else
			plural = word .. "s"
		end
		result[#result+1] = word
		result[#result+1] = plural
	end
	return result
end

-- Build a table of constants suitable to be passed to 'setcase' as argument.
local function append_constants(const, new)
	if type(const) ~= 'table' then
		const = {}
	end
	for _, word in ipairs(new) do
		const[word:upper()] = word
	end
	return const
end

local debug_output = {
	const = {},
	roman = {},
	mixed = {},
	macx = {},
}

-- "Constants" are written as provided, except if they begin a sentence in which
-- case the first letter is uppercase.
--
-- * Roman numerals are left as is. If lowercase, they are not considered as
-- roman numerals to prevent conflict with common words.
--
-- * Names like D'Arcy, O'Reilly, McDonald and MacNeil are properly handled.
--
-- Options:
--
-- sentencecase: when set to true, only the first letter of every sentence will
-- be capitalized, the other words that are not subject to the rules will be
-- lowercase.
--
-- This script was inspired by http://www.pement.org/awk/titlecase.awk.txt.
local function setcase(input, const, sentencecase)
	-- Process words from 'input' one by one and append them to 'output'.
	local output = {}

	-- Digits and apostrophes are considered part of a word. There are different
	-- symbols for apostrophe, some of them which are not ASCII. Apostrophes are
	-- assumed not to start or end the word, so that to avoid confusion with
	-- quotes.
	for nonword, word in input:gmatch([[([^\pL\pN]*)([\pL\pN][\pL\pN'´’]*[\pL\pN]|[\pL\pN])]]) do
		-- The uppercase/lowercase versions are used to ease matching and save some
		-- function calls.

		local upper = word:upper()
		local lower = word:lower()
		-- Append non-word chars preceding 'word' to 'output'.
		table.insert(output, nonword)

		-- Control if 'word' should be matched or not.
		local unmatched = true

		-- Rule 1: Constant strings.
		local var = const[upper]
		if var then
			word = const[upper] or word
			debug_output['const'][#debug_output['const']+1] = word
			unmatched = false
		end

		-- Rule 2: Roman numerals.
		-- If 'word' matches with uppercase roman numerals we keep it. We assume
		-- roman numerals are already uppercase in the input, otherwise we cannot
		-- distinguish between normal words and numerals (e.g. Liv, civil, did, dim,
		-- lid, mid-, mild, Vic).
		if unmatched and word:match('^[IVXLCDM]+$') then
			unmatched = false
			debug_output['roman'][#debug_output['roman']+1] = word
		end

		-- Rule 3: Names like D'Arcy or O'Reilly.
		-- If 'sentencecase', we do not process this rule: this is helpful for
		-- languages like French where "d'" appears a lot.
		if unmatched and not sentencecase and upper:match([=[^[DO]['´’][\pL\pN]]=]) then
			word = upper:sub(1, 3) .. lower:sub(4)
			unmatched = false
			debug_output['mixed'][#debug_output['mixed']+1] = word
		end

		-- Rule 4: Names like MacNeil or McDonald.
		if unmatched and upper:match('^MA?C[B-DF-HJ-NP-TV-Z]') then
			unmatched = false
			debug_output['macx'][#debug_output['macx']+1] = word
			if upper:sub(2, 2) == 'A' then
				word = upper:sub(1, 1) .. 'ac' .. upper:sub(4, 4) .. lower:sub(5)
			else
				word = upper:sub(1, 1) .. 'c' .. upper:sub(3, 3) .. lower:sub(4)
			end
		end

		-- If one of the above rule is hit, we append the resulting 'word' as is to
		-- 'output', otherwise we capitalize/lowercase it.
		if not unmatched then table.insert(output, word)
		elseif sentencecase then table.insert(output, lower)
		else table.insert(output, upper:sub(1, 1) .. lower:sub(2))
		end
	end

	-- Append remaining non-word chars to 'output'.
	table.insert(output, input:match([[([^\pL\pN]*)$]]))
	-- Everything should be converted now.
	output = table.concat(output)

	-- Exception 1: Capitalize first word. This is needed in case the string
	-- starts with a lowercase constant.
	output = output:gsub([=[[\pL\pN]]=], function (c) return c:upper() end, 1)

	-- Exception 2: Capitalize first word after some punctuation marks.
	output = output:gsub([[([{}[\]?!():.-/][^\pL\pN]*)(\p{Ll})]], function (r, c) return r .. c:upper() end)

	-- Exception 3: Capitalize first word right after a quote.
	output = output:gsub([[([^\pL\pN]["'´’])(\p{Ll})]], function (r, c) return r .. c:upper() end)

	return output
end

local constants = {}
constants = append_constants(constants, const_en)
constants = append_constants(constants, const_music)
constants = append_constants(constants, const_common)
constants = append_constants(constants, const_units)
constants = append_constants(constants, const_custom)

local const_mac_pl = pluralize(const_mac)
if sentencecase then
	for _, word in ipairs(const_mac_pl) do
		constants[word:upper()] = word:lower()
	end
else
	constants = append_constants(constants, const_mac_pl)
end

for k, v in pairs(output.tags) do
	output.tags[k] = setcase(v, constants, sentencecase)
end

local debug_str = ""
for k, v in pairs(debug_output) do
	if v ~= nil then
		local str = table.concat(v, " ")
		if #str ~= 0 then
			debug_str = debug_str .. " " .. k .. "={" .. str .. "}"
		end
	end
end
if #debug_str ~= 0 then
	debug("Script case:" .. debug_str)
end
