// Copyright © 2013-2016 Pierre Neidhardt <ambrevar@gmail.com>
// Use of this file is governed by the license that can be found in LICENSE.

// TODO: Enforce field/type consistency in 'output'?

// Convert 'input' and 'output' from Go to Lua and from Lua to Go. Almost all
// scripting support is implemented in this file: in case of library change,
// this is the only file that would need some overhaul.

package main

import (
	"fmt"
	"log"
	"reflect"

	"bitbucket.org/ambrevar/demlo/golua/unicode"
	"github.com/aarzilli/golua/lua"
	"github.com/ambrevar/luar"
)

const (
	registryWhitelist = "_whitelist"
	registryScripts   = "_scripts"
)

// goToLua copies Go values to Lua and sets the result to name.
// Compound types are deep-copied.
// Functions are automatically converted to 'func (L *lua.State) int'.
func goToLua(L *lua.State, name string, val interface{}) {
	t := reflect.TypeOf(val)
	if t.Kind() == reflect.Func {
		L.PushGoFunction(luar.GoLuaFunc(L, val))
	} else {
		luar.GoToLua(L, t, reflect.ValueOf(val), true)
	}
	L.SetGlobal(name)
}

// Registers a Go function as a global variable and add it to the sandbox.
func sandboxRegister(L *lua.State, name string, f interface{}) {
	goToLua(L, name, f)

	L.PushString(registryWhitelist)
	L.GetTable(lua.LUA_REGISTRYINDEX)
	L.GetGlobal(name)
	L.SetField(-2, name)
}

// The caller is responsible for closing the Lua state.
// Add a `defer L.Close()` to the calling code if there is no error.
func makeSandbox(logPrint func(v ...interface{})) (*lua.State, error) {
	L := lua.NewState()
	L.OpenLibs()
	unicode.GoLuaReplaceFuncs(L)

	// Store the whitelist in registry to avoid tampering it.
	L.PushString(registryWhitelist)
	err := L.DoString(luaWhitelist)
	if err != nil {
		log.Fatal("Spurious sandbox", err)
	}
	L.SetTable(lua.LUA_REGISTRYINDEX)

	// Register before setting up the sandbox: these functions will be restored
	// together with the sandbox.
	// The closure allows access to the external logger.
	luaDebug := func(L *lua.State) int { return 0 }
	if logPrint != nil {
		luaDebug = func(L *lua.State) int {
			var arglist []interface{}
			nargs := L.GetTop()
			for i := 1; i <= nargs; i++ {
				if L.IsString(i) {
					arglist = append(arglist, L.ToString(i))
				}
			}
			logPrint(arglist...)
			return 0
		}
	}

	sandboxRegister(L, "debug", luaDebug)
	sandboxRegister(L, "stringnorm", stringNorm)
	sandboxRegister(L, "stringrel", stringRel)

	// Purge _G from everything but the content of the whitelist.
	err = L.DoString(luaSetSandbox)
	if err != nil {
		log.Fatal("Cannot load function to set sandbox", err)
	}
	L.PushString(registryWhitelist)
	L.GetTable(lua.LUA_REGISTRYINDEX)
	err = L.Call(1, 0)
	if err != nil {
		log.Fatal("Failed to set sandbox", err)
	}

	return L, nil
}

func sandboxCompileScripts(L *lua.State, scripts []scriptBuffer) {
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
}

func makeSandboxOutput(L *lua.State, output *outputInfo) {
	goToLua(L, "output", *output)
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
			// Use 'key' at index -2 and 'value' at index -1.
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
	err := L.DoString(luaRestoreSandbox)
	if err != nil {
		log.Fatal("Cannot load function to restore sandbox", err)
	}
	L.PushString(registryWhitelist)
	L.GetTable(lua.LUA_REGISTRYINDEX)
	err = L.Call(1, 0)
	if err != nil {
		log.Fatal("Failed to restore sandbox", err)
	}

	goToLua(L, "input", *input)

	// Shortcut (mostly for prescript and postscript).
	L.GetGlobal("input")
	L.GetField(-1, "tags")
	L.SetGlobal("i")
	L.Pop(1)
	L.GetGlobal("output")
	L.GetField(-1, "tags")
	L.SetGlobal("o")
	L.Pop(1)

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

func scriptOutput(L *lua.State) (output outputInfo) {
	L.GetGlobal("output")
	r := luar.LuaToGo(L, reflect.TypeOf(output), -1)
	L.Pop(1)

	output = r.(outputInfo)

	return output
}

func loadConfig(config string) (o optionSet) {
	L, err := makeSandbox(log.Println)
	defer L.Close()

	// Load config.
	err = L.DoFile(config)
	if err != nil {
		log.Fatalf("Error loading config: %s", err)
	}

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
