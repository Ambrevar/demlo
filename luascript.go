// Copyright © 2013-2016 Pierre Neidhardt <ambrevar@gmail.com>
// Use of this file is governed by the license that can be found in LICENSE.

// TODO: Make Go<->Lua conversions dynamic, à-la-json.
// TODO: Enforce field/type consistency in 'output'?

// Convert 'input' and 'output' from Go to Lua and from Lua to Go. Almost all
// scripting support is implemented in this file: in case of library change,
// this is the only file that would need some overhaul.

package main

import (
	"fmt"
	"log"
	"os"

	"bitbucket.org/ambrevar/demlo/golua/unicode"
	"github.com/aarzilli/golua/lua"
)

const (
	registryRestoreSandbox = "_restore_sandbox"
	registrySandbox        = "_sandbox"
	registryScripts        = "_scripts"
)

func luaStringNorm(L *lua.State) int {
	if !L.IsString(-1) {
		L.PushString("")
	} else {
		L.PushString(stringNorm(L.ToString(-1)))
	}
	return 1
}

func luaStringRel(L *lua.State) int {
	if !L.IsString(-2) || !L.IsString(-1) {
		L.PushNumber(0.0)
	} else {
		L.PushNumber(stringRel(L.ToString(-2), L.ToString(-1)))
	}
	return 1
}

// t[k] = v, where k is a string and t is at the top of the stack.
func setMap(L *lua.State, key string, value interface{}) {
	switch i := value.(type) {
	case int:
		L.PushInteger(int64(i))
	case string:
		L.PushString(i)
	default:
		return
	}
	L.SetField(-2, key)
}

// t[k] = v, where k is an integer and t is at the top of the stack.
func setArray(L *lua.State, key int, value interface{}) {
	L.PushInteger(int64(key))
	switch i := value.(type) {
	case int:
		L.PushInteger(int64(i))
	case string:
		L.PushString(i)
	default:
		return
	}
	L.SetTable(-3)
}

func lua2goOutCover(L *lua.State) outputCover {
	var out outputCover

	L.GetField(-1, "path")
	if L.IsString(-1) {
		out.Path = L.ToString(-1)
	}
	L.Pop(1)

	L.GetField(-1, "format")
	if L.IsString(-1) {
		out.Format = L.ToString(-1)
	}
	L.Pop(1)

	L.GetField(-1, "parameters")
	if L.IsTable(-1) {
		for i := 1; ; i++ {
			L.PushInteger(int64(i))
			L.GetTable(-2)
			if L.IsNil(-1) {
				L.Pop(1)
				break
			}
			if L.IsString(-1) {
				out.Parameters = append(out.Parameters, L.ToString(-1))
			}
			L.Pop(1)
		}
	}
	L.Pop(1)

	return out
}

// The caller is responsible for closing the Lua state.
// Add a `defer L.Close()` to the calling code if there is no error.
func makeSandbox(scripts []scriptBuffer, scriptLog *log.Logger) (*lua.State, error) {
	L := lua.NewState()
	L.OpenLibs()

	// Register before defining the sandbox: these functions will be restored
	// together with the sandbox.
	// The closure allows access to the script logger.
	luaDebug := func(L *lua.State) int {
		return 0
	}
	if options.debug {
		luaDebug = func(L *lua.State) int {
			var arglist []interface{}
			nargs := L.GetTop()
			for i := 1; i <= nargs; i++ {
				if L.IsString(i) {
					arglist = append(arglist, L.ToString(i))
				}
			}
			scriptLog.Println(arglist...)
			return 0
		}
	}
	L.Register("debug", luaDebug)
	L.Register("stringnorm", luaStringNorm)
	L.Register("stringrel", luaStringRel)

	unicode.GoLuaReplaceFuncs(L)

	// Enclose L in the sandbox. See 'sandbox.go'.
	err := L.DoString(sandbox)
	if err != nil {
		log.Fatal("Spurious sandbox", err)
	}

	// Store the sandbox in registry and remove it from _G to avoid tampering it.
	L.PushString(registrySandbox)
	L.GetGlobal("_sandbox")
	L.SetTable(lua.LUA_REGISTRYINDEX)
	L.PushNil()
	L.SetGlobal("_sandbox")

	L.PushString(registryRestoreSandbox)
	L.GetGlobal("_restore_sandbox")
	L.SetTable(lua.LUA_REGISTRYINDEX)
	L.PushNil()
	L.SetGlobal("_restore_sandbox")

	// Compile scripts.
	L.PushString(registryScripts)
	L.NewTable()
	for _, script := range scripts {
		L.PushString(script.path)
		err := L.LoadString(script.buf)
		if err != 0 {
			log.Fatalf("%s: %s", script.path, L.ToString(-1))
			L.Pop(2)
		} else {
			L.SetTable(-3)
		}
	}
	L.SetTable(lua.LUA_REGISTRYINDEX)

	return L, nil
}

func makeSandboxInput(L *lua.State, input *inputInfo) {
	L.NewTable()

	setMap(L, "path", input.path)
	setMap(L, "bitrate", input.bitrate)
	setMap(L, "audioindex", input.audioIndex)

	L.NewTable()
	for k, v := range input.tags {
		setMap(L, k, v)
	}
	L.SetField(-2, "tags")

	// Embedded covers.
	L.NewTable()
	for index, v := range input.embeddedCovers {
		L.PushInteger(int64(index + 1))
		L.NewTable()
		setMap(L, "format", v.format)
		setMap(L, "width", v.width)
		setMap(L, "height", v.height)
		setMap(L, "checksum", v.checksum)
		L.SetTable(-3)
	}
	L.SetField(-2, "embeddedcovers")

	// External covers.
	L.NewTable()
	for file, v := range input.externalCovers {
		L.NewTable()
		setMap(L, "format", v.format)
		setMap(L, "width", v.width)
		setMap(L, "height", v.height)
		setMap(L, "checksum", v.checksum)
		L.SetField(-2, file)
	}
	L.SetField(-2, "externalcovers")

	// Online cover.
	L.NewTable()
	setMap(L, "format", input.onlineCover.format)
	setMap(L, "width", input.onlineCover.width)
	setMap(L, "height", input.onlineCover.height)
	setMap(L, "checksum", input.onlineCover.checksum)
	L.SetField(-2, "onlinecover")

	// Streams.
	L.NewTable()
	for index, v := range input.Streams {
		L.PushInteger(int64(index + 1))
		L.NewTable()
		setMap(L, "bit_rate", v.Bitrate)
		setMap(L, "codec_name", v.CodecName)
		setMap(L, "codec_type", v.CodecType)
		setMap(L, "duration", v.Duration)
		setMap(L, "height", v.Height)
		setMap(L, "width", v.Width)

		L.NewTable()
		for k, v := range v.Tags {
			setMap(L, k, v)
		}
		L.SetField(-2, "tags")

		L.SetTable(-3)
	}
	L.SetField(-2, "streams")

	// Format.
	L.NewTable()
	setMap(L, "bit_rate", input.Format.Bitrate)
	setMap(L, "duration", input.Format.Duration)
	setMap(L, "format_name", input.Format.FormatName)
	setMap(L, "nb_streams", input.Format.NbStreams)
	L.NewTable()
	for k, v := range input.Format.Tags {
		setMap(L, k, v)
	}
	L.SetField(-2, "tags")
	L.SetField(-2, "format")

	L.SetGlobal("input")

	// Shortcut (mostly for prescript and postscript).
	L.GetGlobal("input")
	L.GetField(-1, "tags")
	L.SetGlobal("i")
	L.Pop(1)
}

func makeSandboxOutput(L *lua.State, output *outputInfo) {
	L.NewTable()

	setMap(L, "path", output.Path)
	setMap(L, "format", output.Format)

	L.NewTable()
	for k, v := range output.Tags {
		setMap(L, k, v)
	}
	L.SetField(-2, "tags")

	L.NewTable()
	for k, v := range output.Parameters {
		setArray(L, k+1, v)
	}
	L.SetField(-2, "parameters")

	// Embedded covers.
	L.NewTable()
	for index, v := range output.EmbeddedCovers {
		L.PushInteger(int64(index + 1))
		L.NewTable()

		setMap(L, "path", v.Path)
		setMap(L, "format", v.Format)

		L.NewTable()
		for k, v := range v.Parameters {
			setArray(L, k+1, v)
		}
		L.SetField(-2, "parameters")

		L.SetTable(-3)
	}
	L.SetField(-2, "embeddedcovers")

	// External covers.
	L.NewTable()
	for file, v := range output.ExternalCovers {
		L.NewTable()

		setMap(L, "path", v.Path)
		setMap(L, "format", v.Format)

		L.NewTable()
		for k, v := range v.Parameters {
			setArray(L, k+1, v)
		}
		L.SetField(-2, "parameters")

		L.SetField(-2, file)
	}
	L.SetField(-2, "externalcovers")

	// Online cover.
	L.NewTable()
	setMap(L, "path", output.OnlineCover.Path)
	setMap(L, "format", output.OnlineCover.Format)
	L.NewTable()
	for k, v := range output.OnlineCover.Parameters {
		setArray(L, k+1, v)
	}
	L.SetField(-2, "parameters")
	L.SetField(-2, "onlinecover")

	L.SetGlobal("output")

	// Shortcut (mostly for prescript and postscript).
	L.GetGlobal("output")
	L.GetField(-1, "tags")
	L.SetGlobal("o")
	L.Pop(1)
}

// The user is responsible for ensuring the integrity of 'output'. We convert
// numbers to strings in tags for convenience.
func sanitizeOutput(L *lua.State) {
	L.GetGlobal("output")

	if !L.IsTable(-1) {
		L.NewTable()
		L.SetGlobal("output")
	}

	L.GetField(-1, "tags")
	if L.IsTable(-1) {
		// First key.
		L.PushNil()
		for L.Next(-2) != 0 {
			// Use 'key' (at index -2) and 'value' (at index -1)
			if L.IsString(-2) && L.IsString(-1) {
				// Convert numbers to strings.
				L.ToString(-1)
				L.SetField(-3, L.ToString(-2))
			} else {
				// Remove 'value' and keep 'key' for next iteration.
				L.Pop(1)
			}
		}
	}
	L.Pop(1)

	L.Pop(1)
}

func runScript(L *lua.State, script string, input *inputInfo) error {
	// Restore the sandbox.
	L.PushString(registryRestoreSandbox)
	L.GetTable(lua.LUA_REGISTRYINDEX)
	L.PushString(registrySandbox)
	L.GetTable(lua.LUA_REGISTRYINDEX)
	err := L.Call(1, 0)
	if err != nil {
		log.Fatal("Spurious sandbox", err)
	}

	makeSandboxInput(L, input)
	sanitizeOutput(L)

	// Call the compiled script.
	L.PushString(registryScripts)
	L.GetTable(lua.LUA_REGISTRYINDEX)
	L.PushString(script)
	if L.IsTable(-2) {
		L.GetTable(-2)
		if L.IsFunction(-1) {
			err := L.Call(0, 0)
			if err != nil {
				L.SetTop(0)
				return fmt.Errorf("%s", err)
			}
		} else {
			L.Pop(1)
		}
	} else {
		L.Pop(1)
	}
	L.Pop(1)

	return nil
}

func scriptOutput(L *lua.State) outputInfo {
	var output outputInfo
	output.Tags = make(map[string]string)
	output.ExternalCovers = make(map[string]outputCover)

	L.GetGlobal("output")
	if !L.IsTable(-1) {
		return output
	}

	L.GetField(-1, "path")
	if L.IsString(-1) {
		output.Path = L.ToString(-1)
	}
	L.Pop(1)

	L.GetField(-1, "format")
	if L.IsString(-1) {
		output.Format = L.ToString(-1)
	}
	L.Pop(1)

	L.GetField(-1, "tags")
	if L.IsTable(-1) {
		// First key.
		L.PushNil()
		for L.Next(-2) != 0 {
			// Use 'key' (at index -2) and 'value' (at index -1)
			if L.IsString(-2) && L.IsString(-1) {
				output.Tags[L.ToString(-2)] = L.ToString(-1)
			}
			// Remove 'value' and keep 'key' for next iteration.
			L.Pop(1)
		}
	}
	L.Pop(1)

	L.GetField(-1, "parameters")
	if L.IsTable(-1) {
		for i := 1; ; i++ {
			L.PushInteger(int64(i))
			L.GetTable(-2)
			if L.IsNil(-1) {
				L.Pop(1)
				break
			}
			if L.IsString(-1) {
				output.Parameters = append(output.Parameters, L.ToString(-1))
			}
			L.Pop(1)
		}
	}
	L.Pop(1)

	L.GetField(-1, "externalcovers")
	if L.IsTable(-1) {
		// First key.
		L.PushNil()
		for L.Next(-2) != 0 {
			// Use 'key' (at index -2) and 'value' (at index -1).
			if L.IsString(-2) && L.IsTable(-1) {
				output.ExternalCovers[L.ToString(-2)] = lua2goOutCover(L)
			}
			// Remove 'value' and keep 'key' for next iteration.
			L.Pop(1)
		}
	}
	L.Pop(1)

	L.GetField(-1, "embeddedcovers")
	if L.IsTable(-1) {
		for i := 1; ; i++ {
			L.PushInteger(int64(i))
			L.GetTable(-2)
			if L.IsNil(-1) {
				L.Pop(1)
				break
			}
			output.EmbeddedCovers = append(output.EmbeddedCovers, lua2goOutCover(L))
			L.Pop(1)
		}
	}
	L.Pop(1)

	L.GetField(-1, "onlinecover")
	if L.IsTable(-1) {
		output.OnlineCover = lua2goOutCover(L)
	}
	L.Pop(1)

	// Remove 'output' from the stack.
	L.Pop(1)

	return output
}

func loadConfig(config string) optionSet {
	L := lua.NewState()
	defer L.Close()
	L.OpenLibs()

	// Register before defining the sandbox.
	luaDebug := func(L *lua.State) int {
		var arglist []interface{}
		nargs := L.GetTop()
		for i := 1; i <= nargs; i++ {
			if L.IsString(i) {
				arglist = append(arglist, L.ToString(i))
			}
		}
		fmt.Fprintln(os.Stderr, arglist...)
		return 0
	}
	L.Register("debug", luaDebug)
	L.Register("stringnorm", luaStringNorm)
	L.Register("stringrel", luaStringRel)

	unicode.GoLuaReplaceFuncs(L)

	// Enclose L in the sandbox.
	err := L.DoString(sandbox)
	if err != nil {
		log.Fatal("Spurious sandbox", err)
	}

	// Clean up restoration data: not needed for config since the Lua state will
	// not be reused.
	L.PushNil()
	L.SetGlobal("_sandbox")

	L.PushNil()
	L.SetGlobal("_restore_sandbox")

	// Load config.
	err = L.DoFile(config)
	if err != nil {
		log.Fatalf("Error loading config: %s", err)
	}

	o := optionSet{}

	L.GetGlobal("color")
	o.color = L.ToBoolean(-1)
	L.Pop(1)

	L.GetGlobal("cores")
	o.cores = L.ToInteger(-1)
	L.Pop(1)

	L.GetGlobal("extensions")
	if L.IsTable(-1) {
		o.extensions = stringSetFlag{}
		for i := 1; ; i++ {
			L.PushInteger(int64(i))
			L.GetTable(-2)
			if L.IsNil(-1) {
				L.Pop(1)
				break
			}
			o.extensions[L.ToString(-1)] = true
			L.Pop(1)
		}
	}
	L.Pop(1)

	L.GetGlobal("getcover")
	o.getcover = L.ToBoolean(-1)
	L.Pop(1)

	L.GetGlobal("gettags")
	o.gettags = L.ToBoolean(-1)
	L.Pop(1)

	L.GetGlobal("overwrite")
	o.overwrite = L.ToBoolean(-1)
	L.Pop(1)

	L.GetGlobal("prescript")
	o.prescript = L.ToString(-1)
	L.Pop(1)

	L.GetGlobal("postscript")
	o.postscript = L.ToString(-1)
	L.Pop(1)

	L.GetGlobal("process")
	o.process = L.ToBoolean(-1)
	L.Pop(1)

	L.GetGlobal("removesource")
	o.removesource = L.ToBoolean(-1)
	L.Pop(1)

	L.GetGlobal("scripts")
	if L.IsTable(-1) {
		for i := 1; ; i++ {
			L.PushInteger(int64(i))
			L.GetTable(-2)
			if L.IsNil(-1) {
				L.Pop(1)
				break
			}
			o.scripts = append(o.scripts, L.ToString(-1))
			L.Pop(1)
		}
	}
	L.Pop(1)

	return o
}
