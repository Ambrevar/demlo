// Copyright © 2013-2016 Pierre Neidhardt <ambrevar@gmail.com>
// Use of this file is governed by the license that can be found in LICENSE.

package unicode

// TODO: Add tests for Gmatch.
// TODO: Split tests.

import (
	"os"
	"testing"

	"github.com/aarzilli/golua/lua"
)

var L = lua.NewState()

func init() {
	L.OpenLibs()
	GoLuaReplaceFuncs(L)
}

func TestAll(t *testing.T) {
	tdt := []struct {
		code   string
		result []string
	}{
		// Find
		{code: `string.find('wabbbcccz', 'a(b*(c*)(z))')`, result: []string{"2", "9", "bbbcccz", "ccc", "z"}},
		{code: `string.find('aaa', '(a*)')`, result: []string{"1", "3", "aaa"}},
		{code: `string.find('bbb', '(a*)', 4)`, result: []string{"4", "3", ""}},
		{code: `string.find('aaa', '(a*)', 5)`, result: []string{}},
		{code: `string.find('bbb', '(a*)')`, result: []string{"1", "0", ""}},
		{code: `string.find('bbb', '(a+)')`, result: []string{}},
		{code: `string.find('aaa', 'a*')`, result: []string{"1", "3"}},

		// Gsub
		{code: `string.gsub("hello world", "(hello|world)", "$1 $1")`, result: []string{"hello hello world world", "2"}},
		{code: `string.gsub("home = $HOME, user = $USER", "\\$([A-Z]+)", os.getenv)`, result: []string{"home = " + os.Getenv("HOME") + ", user = " + os.Getenv("USER"), "2"}},
		{code: `string.gsub("home = $HOME, user = $USER", "\\$([A-Z]+)", os.getenv, -1)`, result: []string{"home = $HOME, user = $USER", "0"}},
		{code: `string.gsub("a", "b", "v")`, result: []string{"a", "0"}},
		{code: `string.gsub("ab", "(a|b)", {a="A", b="B"})`, result: []string{"AB", "2"}},
		{code: `string.gsub("ab-ab", "(a)(b)", function (a, b) return a:upper() .. b:upper() end)`, result: []string{"AB-AB", "2"}},
		{code: `string.gsub("ab-ab", "(a)(b)", function () return nil end)`, result: []string{"ab-ab", "2"}},

		// Match
		{code: `string.match('wabbbcccz', 'a(b*(c*)(z))')`, result: []string{"bbbcccz", "ccc", "z"}},
		{code: `string.match('aaa', '(a*)')`, result: []string{"aaa"}},
		{code: `string.match('bbb', '(a*)', 4)`, result: []string{""}},
		{code: `string.match('aaa', '(a*)', 5)`, result: []string{}},
		{code: `string.match('bbb', '(a*)')`, result: []string{""}},
		{code: `string.match('bbb', '(a+)')`, result: []string{}},
		{code: `string.match('aaa', 'a*')`, result: []string{"aaa"}},

		// Sub
		{code: `string.sub('bar', 0)`, result: []string{"bar"}},
		{code: `string.sub('bar', 1)`, result: []string{"bar"}},
		{code: `string.sub('bar', 2)`, result: []string{"ar"}},
		{code: `string.sub('bar', 3)`, result: []string{"r"}},
		{code: `string.sub('bar', 4)`, result: []string{""}},
		{code: `string.sub('bar', -1)`, result: []string{"r"}},
		{code: `string.sub('bar', 0, 2)`, result: []string{"ba"}},
		{code: `string.sub('bar', 2, 0)`, result: []string{""}},
		{code: `string.sub('bar', 1, 2)`, result: []string{"ba"}},
		{code: `string.sub('bar', -5, 10)`, result: []string{"bar"}},
	}

	for _, want := range tdt {
		err := L.DoString("result = {" + want.code + "}")
		if err != nil {
			t.Fatal("Error in test data")
		}
		L.GetGlobal("result")
		count := 0
		for i := 1; ; i++ {
			L.PushInteger(int64(i))
			L.GetTable(-2)
			if L.IsNil(-1) {
				break
			}
			if i > len(want.result) {
				t.Fatalf("Got at least %v results, want %v", i, len(want.result))
			}
			got := L.ToString(-1)
			if got != want.result[i-1] {
				t.Errorf("Got %v, want %v", got, want.result[i-1])
			}
			L.Pop(1)
			count++
		}
		if count < len(want.result) {
			t.Errorf("Got %v results, want %v", count, len(want.result))
		}
	}
}
