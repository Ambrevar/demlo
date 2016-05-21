// Copyright © 2013-2016 Pierre Neidhardt <ambrevar@gmail.com>
// Use of this file is governed by the license that can be found in LICENSE.

package main

import (
	"regexp"
	"strings"

	"github.com/jhprks/damerau"
)

var (
	RE_NORM = regexp.MustCompile(`\b0+|[^\pL\pN]`)
)

// Remove punctuation and padding zeros for number comparisons. Return the
// result in lowercase. This is useful to make string relations more relevant.
func stringNorm(s string) string {
	return strings.ToLower(RE_NORM.ReplaceAllString(s, ""))
}

// Return the Damerau-Levenshtein distance divided by the length of the longest
// string, so that two identical strings return 1, and two completely unrelated
// strings return 0.
func stringRel(a, b string) float64 {
	max := len([]rune(a))
	if len([]rune(b)) > max {
		max = len([]rune(b))
	} else if max == 0 {
		return 1
	}

	distance := damerau.DamerauLevenshteinDistance(a, b)
	return 1 - float64(distance)/float64(max)
}
