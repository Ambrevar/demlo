// Copyright © 2013-2016 Pierre Neidhardt <ambrevar@gmail.com>
// Use of this file is governed by the license that can be found in LICENSE.

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
	"github.com/stevedonovan/luar"
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
func MakeSandbox(logPrint func(v ...interface{})) (*lua.State, error) {
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

// SandboxCompileScripts transfers the script buffer to the Lua state L and
// references them in LUA_REGISTRYINDEX.
func SandboxCompileScripts(L *lua.State, scripts []scriptBuffer) {
	L.PushString(registryScripts)
	L.NewTable()
	for _, script := range scripts {
		L.PushString(script.name)
		err := L.LoadString(script.buf)
		if err != 0 {
			log.Fatalf("%s: %s", script.name, L.ToString(-1))
			L.Pop(2)
		} else {
			L.SetTable(-3)
		}
	}
	L.SetTable(lua.LUA_REGISTRYINDEX)
}

func outputNumbersToStrings(L *lua.State) {
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

// RunScript executes script named 'script' with 'input' and 'output' set as global variable.
// Any change made to 'input' is discarded. Change to 'output' are transfered
// back to Go on every script call to guarantee type consistency across script
// calls (Lua is dynamically typed).
func RunScript(L *lua.State, script string, input *inputInfo, output *outputInfo) error {
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
	goToLua(L, "output", *output)

	// Shortcut (mostly for prescript and postscript).
	L.GetGlobal("input")
	L.GetField(-1, "tags")
	L.SetGlobal("i")
	L.Pop(1)
	L.GetGlobal("output")
	L.GetField(-1, "tags")
	L.SetGlobal("o")
	L.Pop(1)

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

	// We allow tags to be numbers for convenience.
	outputNumbersToStrings(L)

	L.GetGlobal("output")
	r := luar.LuaToGo(L, reflect.TypeOf(*output), -1)
	L.Pop(1)

	*output = r.(outputInfo)

	return nil
}

func LoadConfig(config string, options interface{}) {
	L, err := MakeSandbox(log.Println)
	defer L.Close()

	err = L.DoFile(config)
	if err != nil {
		log.Fatalf("Error loading config: %s", err)
	}

	L.GetGlobal("_G")
	r := luar.LuaToGo(L, reflect.TypeOf(options), -1)
	L.Pop(1)

	v := reflect.ValueOf(options)
	v.Elem().Set(reflect.ValueOf(r).Elem())
}
