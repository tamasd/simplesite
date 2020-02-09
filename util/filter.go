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
	"bytes"
	"sync"

	"github.com/microcosm-cc/bluemonday"
	"github.com/sirupsen/logrus"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

// Filter represents a generic markdown filter.
type Filter struct {
	logger logrus.FieldLogger
	policy *bluemonday.Policy
	md     goldmark.Markdown
	pool   sync.Pool
}

// NewFilter creates a filter with a logger.
func NewFilter(logger logrus.FieldLogger) *Filter {
	return &Filter{
		logger: logger,
		policy: bluemonday.UGCPolicy(),
		md: goldmark.New(
			goldmark.WithExtensions(extension.GFM),
			goldmark.WithParserOptions(
				parser.WithAutoHeadingID(),
			),
			goldmark.WithRendererOptions(
				html.WithHardWraps(),
				html.WithXHTML(),
			),
		),
		pool: sync.Pool{New: func() interface{} {
			return bytes.NewBuffer(nil)
		}},
	}
}

// Filter converts markdown to safe HTML.
func (f *Filter) Filter(input string) string {
	buf := f.pool.Get().(*bytes.Buffer)
	defer func() {
		buf.Reset()
		f.pool.Put(buf)
	}()

	// An error won't happen here, because *bytes.Buffer never returns an error.
	_ = f.md.Convert([]byte(input), buf)

	return f.policy.Sanitize(buf.String())
}
