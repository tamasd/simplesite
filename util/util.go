// A simple website in Go.
// Copyright (c) 2020. Tamás Demeter-Haludka
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package util

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

var (
	matchFirstCap = regexp.MustCompile("(.)([A-Z][a-z]+)")
	matchAllCap   = regexp.MustCompile("([a-z0-9])([A-Z])")
)

// ToSnakeCase converts camel case to snake case.
func ToSnakeCase(str string) string {
	snake := matchFirstCap.ReplaceAllString(str, "${1}_${2}")
	snake = matchAllCap.ReplaceAllString(snake, "${1}_${2}")
	return strings.ToLower(snake)
}

// SetContext sets a value in a request's context.
func SetContext(r *http.Request, key, value interface{}) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), key, value))
}

// RandomHexString returns a random hex string with a given string length.
func RandomHexString(length int) string {
	buflen := length / 2

	if length%2 == 1 {
		buflen++
	}

	buf := make([]byte, buflen)

	_, _ = io.ReadFull(rand.Reader, buf)

	return hex.EncodeToString(buf)[:length]
}

// GeneratePlaceholders generates placeholders for an SQL query.
func GeneratePlaceholders(start, length int) string {
	if length == 0 {
		return ""
	}

	var str string
	for i := 0; i < length; i++ {
		str += ", $" + strconv.Itoa(i+start)
	}

	return str[2:]
}
