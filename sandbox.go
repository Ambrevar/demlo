// Copyright © 2013-2016 Pierre Neidhardt <ambrevar@gmail.com>
// Use of this file is governed by the license that can be found in LICENSE.

package main

var sandbox = `
_sandbox = {
	-- demlo specific.
	debug = debug,
	stringnorm = stringnorm,
	stringrel = stringrel,

	-- golua specific: pcall and xpcall are unsafe, we do not add them to the sand box. Coroutines might not be supported, do not include them either.
	-- golua comes with its own message handler:
	golua_default_msghandler = golua_default_msghandler,

	-- Standard Lua.
	assert = assert,
	ipairs = ipairs,
	error = error,
	getmetatable = getmetatable,
	next = next,
	pairs = pairs,
	select = select,
	rawequal = rawequal,
	rawget = rawget,
	rawset = rawset,
	setmetatable = setmetatable,
	tonumber = tonumber,
	tostring = tostring,
	type = type,
	unpack = unpack, -- Deprecated in Lua 5.2
	_VERSION = _VERSION,
	math = {
		abs = math.abs,
		acos = math.acos,
		asin = math.asin,
		atan = math.atan,
		atan2 = math.atan2,
		ceil = math.ceil,
		cos = math.cos,
		cosh = math.cosh,
		deg = math.deg,
		exp = math.exp,
		floor = math.floor,
		fmod = math.fmod,
		frexp = math.frexp,
		huge = math.huge,
		ldexp = math.ldexp,
		log = math.log,
		log10 = math.log10, -- Deprecated in Lua 5.2
		max = math.max,
		min = math.min,
		modf = math.modf,
		pi = math.pi,
		pow = math.pow,
		rad = math.rad,
		random = math.random,
		randomseed = math.randomseed,
		sin = math.sin,
		sinh = math.sinh,
		sqrt = math.sqrt,
		tan = math.tan,
		tanh = math.tanh,
	},
	os = {
		clock = os.clock,
		date = os.date,
		difftime = os.difftime,
		getenv = os.getenv,
		time = os.time,
		tmpname = os.tmpname,
	},
	string = {
		byte = string.byte,
		char = string.char,
		find = string.find,
		format = string.format,
		gmatch = string.gmatch,
		gsub = string.gsub,
		len = string.len,
		lower = string.lower,
		match = string.match,
		rep = string.rep,
		reverse = string.reverse,
		sub = string.sub,
		upper = string.upper,
	},
	table = {
		concat = table.concat,
		insert = table.insert,
		maxn = table.maxn, -- Deprecated in Lua 5.2
		remove = table.remove,
		sort = table.sort,
		unpack = table.unpack, -- Lua 5.2
	},
}

function _restore_sandbox(t)
	for k, v in pairs(t) do
		if type(v) == 'table' then
			_G[k]={}
			for ks, vs in pairs(v) do
				_G[k][ks] = vs
			end
		else
			_G[k] = v
		end
	end
end

-- Purge _G.
for k, v in pairs(_G) do
	if k ~= '_G' and v ~= _sandbox and v ~= _restore_sandbox then
		if not _sandbox[k] then
			_G[k] = nil
		elseif type(v) == 'table' then
			for ks in pairs(v) do
				if not _sandbox[k][ks] then
					v[ks] = nil
				end
			end
		end
	end
end
`
