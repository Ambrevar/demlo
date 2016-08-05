// Copyright © 2013-2016 Pierre Neidhardt <ambrevar@gmail.com>
// Use of this file is governed by the license that can be found in LICENSE.

// Package unicode adds unicode support to some functions in golua's string
// library. Lua patterns are replaced by Go regexps. See
// https://github.com/google/re2/wiki/Syntax.
//
// Use 'GoLuaReplaceFuncs' to register the new functions. It is also possible to
// replace only a subset of those functions manually, or to register these
// functions to a table other than 'string'.
//
// For the full specification of the functions, see
// http://www.lua.org/manual/5.1/manual.html#5.4.
package unicode

import (
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/aarzilli/golua/lua"
)

// TODO: Rename package to avoid clashes?
// TODO: Review Lua official implementation in lstrlib.c.
// TODO: Make package independant.
// TODO: Test how memoization scales with regexpCache.

type entry struct {
	re    *regexp.Regexp
	ready chan struct{}
}

var regexpCache = struct {
	v map[string]*entry
	sync.Mutex
}{v: map[string]*entry{}}

func regexpQuery(L *lua.State, pattern string) *regexp.Regexp {
	regexpCache.Lock()
	e := regexpCache.v[pattern]
	if e == nil {
		e = &entry{ready: make(chan struct{})}
		regexpCache.v[pattern] = e
		regexpCache.Unlock()

		var err error
		e.re, err = regexp.Compile(pattern)
		if err != nil {
			L.RaiseError(err.Error())
		}

		close(e.ready)
	} else {
		regexpCache.Unlock()
		<-e.ready
	}
	return e.re
}

// Warning: The result can be > len(s).
func luaToGoStartIndex(i, length int) int {
	if i > 0 {
		i--
	} else if i < 0 {
		i += length
		if i < 0 {
			i = 0
		}
	}
	return i
}

func luaToGoEndIndex(j, length int) int {
	if j > length {
		j = length
	} else if j < 0 {
		j += length + 1
		if j < 0 {
			j = 0
		}
	}
	return j
}

// Find looks for the first match of pattern in the string s.
func Find(L *lua.State) int {
	str := L.CheckString(1)
	pattern := L.CheckString(2)
	init := L.OptInteger(3, 0)
	init = luaToGoStartIndex(init, len(str))

	if init > len(str) {
		L.PushNil()
		return 1
	}
	str = str[init:]

	plain := false
	if L.GetTop() >= 4 {
		if !L.IsNil(4) {
			plain = true
		}
	}

	if plain {
		pos := strings.Index(str, pattern)
		if pos < 0 {
			L.PushNil()
			return 1
		}
		L.PushInteger(int64(init + pos + 1))
		L.PushInteger(int64(init + pos + len(pattern)))
		return 2
	}

	re := regexpQuery(L, pattern)

	positions := re.FindStringSubmatchIndex(str)
	if len(positions) == 0 {
		L.PushNil()
		return 1
	}
	L.PushInteger(int64(init + positions[0] + 1))
	L.PushInteger(int64(init + positions[1]))
	for n := 1; n < len(positions)/2; n++ {
		L.PushString(str[positions[2*n]:positions[2*n+1]])
	}
	return 1 + len(positions)/2
}

// Iterator function for Gmatch.
// Do not register this function.
// Iterator invariant state: {pos=N, matches={{captures...}, ...}}
func gmatchAux(L *lua.State) int {
	L.SetTop(1)

	if L.IsNil(1) {
		L.PushNil()
		return 1
	}

	L.GetField(1, "pos")
	pos := L.ToInteger(-1) + 1
	L.Pop(1)
	L.PushInteger(int64(pos))
	L.SetField(1, "pos")

	L.GetField(1, "matches")
	L.PushInteger(int64(pos))
	L.GetTable(-2)

	// Remove everything from the stack but the captures. This saves some space to
	// grow the stack by a lot of captures.
	L.Replace(-2)
	L.Replace(-2)
	capturesIndex := L.GetTop()

	if L.IsNil(-1) {
		L.PushNil()
		return 1
	}

	// Put all captures on the stack.
	count := 0
	for i := 1; ; i++ {
		if !L.CheckStack(1) {
			L.RaiseError("too many captures")
		}
		L.PushInteger(int64(i))
		L.GetTable(capturesIndex)
		if L.IsNil(-1) {
			L.Pop(1)
			break
		}
		count++
	}

	return count
}

// Gmatch returns an iterator function that, each time it is called, returns the
// next captures from pattern over string s. If pattern specifies no captures,
// then the whole match is produced in each call.
func Gmatch(L *lua.State) int {
	str := L.CheckString(1)
	pattern := L.CheckString(2)

	// From Lua specification: "For this function, a '^' at the start of a pattern
	// does not work as an anchor, as this would prevent the iteration."
	if pattern[0] == '^' {
		pattern = string(append([]byte(`\`), pattern...))
	}

	re := regexpQuery(L, pattern)

	// Push iterator.
	L.PushGoFunction(gmatchAux)

	matches := re.FindAllStringSubmatch(str, -1)
	if len(matches) == 0 {
		L.PushNil()
		return 2
	}

	// Push state invariant table.
	L.NewTable()
	L.PushInteger(0)
	L.SetField(-2, "pos")

	L.NewTable()
	for matchIndex, captures := range matches {
		L.PushInteger(int64(matchIndex + 1))
		L.NewTable()
		if len(captures) == 1 {
			L.PushInteger(1)
			L.PushString(captures[0])
			L.SetTable(-3)
		} else {
			for i := 1; i < len(captures); i++ {
				L.PushInteger(int64(i))
				L.PushString(captures[i])
				L.SetTable(-3)
			}
		}
		L.SetTable(-3)
	}
	L.SetField(-2, "matches")

	return 2
}

// Gsub returns a copy of s in which all (or the first n, if given) occurrences
// of the pattern have been replaced by a replacement string specified by repl,
// which can be a string, a table, or a function. gsub also returns, as its
// second value, the total number of matches that occurred.
func Gsub(L *lua.State) int {
	str := L.CheckString(1)
	pattern := L.CheckString(2)

	tr := L.Type(3)
	L.Argcheck(tr == lua.LUA_TNUMBER || tr == lua.LUA_TSTRING ||
		tr == lua.LUA_TFUNCTION || tr == lua.LUA_TTABLE, 3, "string/function/table expected")

	// If 'n' is unspecified, replace all matches.
	n := L.OptInteger(4, len(str))
	// Replace 0 matches if n<0.
	if n < 0 {
		n = 0
	}

	re := regexpQuery(L, pattern)

	matches := re.FindAllString(str, n)
	nonMatches := re.Split(str, n+1)

	// Replace matches.
	for key, match := range matches {
		switch tr {

		case lua.LUA_TFUNCTION:
			captures := re.FindStringSubmatch(match)
			L.PushValue(3)
			if len(captures) == 1 {
				L.PushString(captures[0])
				// Lua's stdlib leaves errors unhandled.
				_ = L.Call(1, 1)
			} else {
				if !L.CheckStack(len(captures) - 1) {
					L.RaiseError("too many captures")
				}
				for i := 1; i < len(captures); i++ {
					L.PushString(captures[i])
				}
				// Lua's stdlib leaves errors unhandled.
				L.Call(len(captures)-1, 1)
			}

		case lua.LUA_TTABLE:
			captures := re.FindStringSubmatch(match)
			L.PushValue(3)
			if len(captures) == 1 {
				L.GetField(3, captures[0])
			} else {
				L.GetField(3, captures[1])
			}

		default: // LUA_TNUMBER or LUA_TSTRING
			repl := L.ToString(3)
			matches[key] = re.ReplaceAllString(match, repl)
			continue
		}

		// Check function/table return value.
		if !L.ToBoolean(-1) {
			// Keep original text.
			L.Pop(1)
		} else if !L.IsString(-1) {
			L.RaiseError(fmt.Sprintf("invalid replacement value (a %s)", L.LTypename(-1)))
		} else {
			matches[key] = L.ToString(-1)
			L.Pop(1)
		}

	}

	// Rebuild string.
	var result string
	for i := 0; i < len(matches); i++ {
		result += nonMatches[i] + matches[i]
	}
	result += nonMatches[len(nonMatches)-1]

	// Push result.
	L.PushString(result)
	L.PushInteger(int64(len(matches)))
	return 2
}

// Len receives a string and returns its length.
func Len(L *lua.State) int {
	str := L.CheckString(1)
	L.PushInteger(int64(len([]rune(str))))
	return 1
}

// Lower receives a string and returns a copy of this string with all uppercase
// letters changed to lowercase.
func Lower(L *lua.State) int {
	str := L.CheckString(1)
	L.PushString(strings.ToLower(str))
	return 1
}

// Match looks for the first match of pattern in the string s.
func Match(L *lua.State) int {
	str := L.CheckString(1)
	pattern := L.CheckString(2)
	init := L.OptInteger(3, 0)
	init = luaToGoStartIndex(init, len(str))

	if init > len(str) {
		L.PushNil()
		return 1
	}
	str = str[init:]

	re := regexpQuery(L, pattern)

	captures := re.FindStringSubmatch(str)
	if len(captures) == 0 {
		L.PushNil()
		return 1
	}
	if len(captures) == 1 {
		L.PushString(captures[0])
		return 1
	}
	for i := 1; i < len(captures); i++ {
		L.PushString(captures[i])
	}
	return len(captures) - 1
}

// Reverse returns a string that is the string s reversed.
func Reverse(L *lua.State) int {
	str := L.CheckString(1)
	runes := []rune(str)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	L.PushString(string(runes))
	return 1
}

// Sub returns the substring of s that starts at i and continues until j; i and
// j can be negative. I
//
// For Go slices, indices must be positive, start at 0, and the second index is
// excluded.
func Sub(L *lua.State) int {
	runes := []rune(L.CheckString(1))

	i := luaToGoStartIndex(L.CheckInteger(2), len(runes))
	j := luaToGoEndIndex(L.OptInteger(3, len(runes)), len(runes))

	if j <= i {
		L.PushString("")
		return 1
	}
	L.PushString(string(runes[i:j]))
	return 1
}

// Upper receives a string and returns a copy of this string with all lowercase
// letters changed to uppercase.
func Upper(L *lua.State) int {
	str := L.CheckString(1)
	L.PushString(strings.ToUpper(str))
	return 1
}

// GoLuaReplaceFuncs is a helper to replace all supported functions from Lua's
// 'string' library with their unicode counterpart.
func GoLuaReplaceFuncs(L *lua.State) {
	var list = []struct {
		name string
		f    lua.LuaGoFunction
	}{
		{name: "find", f: Find},
		{name: "gmatch", f: Gmatch},
		{name: "gsub", f: Gsub},
		{name: "len", f: Len},
		{name: "lower", f: Lower},
		{name: "match", f: Match},
		{name: "reverse", f: Reverse},
		{name: "sub", f: Sub},
		{name: "upper", f: Upper},
	}

	for _, v := range list {
		L.GetGlobal("string")
		L.PushGoFunction(v.f)
		L.SetField(-2, v.name)
		L.Pop(1)
	}
}

// FlushRegexpCache resets the global regexp cache.
func FlushRegexpCache() {
	regexpCache.Lock()
	regexpCache.v = map[string]*entry{}
	regexpCache.Unlock()
}
