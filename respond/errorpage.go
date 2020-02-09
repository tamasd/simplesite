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

package respond

import (
	"html/template"
	"net/http"

	"github.com/sirupsen/logrus"
	"github.com/tamasd/simplesite/server"
	"github.com/urfave/negroni"
)

const (
	errorPageContextKey = "errorPage"
)

var (
	errorPage = template.Must(template.New("ErrorPage").Parse(`<!DOCTYPE HTML>
<html>
<head>
	<meta http-equiv="X-UA-Compatible" content="IE=edge,chrome=1" />
	<meta charset="utf8" />
	<title>Error</title>
	<style type="text/css">
		body {
			background-color: #dc322f;
			color: #fdf6e3;
		}
	</style>
</head>
<body>
	<h1>HTTP Error {{.Code}}</h1>
	<p>{{.Message}}</p>
</body>
</html>
`))

	panicPage = template.Must(template.New("PanicPage").Parse(`<!DOCTYPE HTML>
<html>
<head>
	<meta http-equiv="X-UA-Compatible" content="IE=edge,chrome=1" />
	<meta charset="utf8" />
	<title>Error</title>
	<style type="text/css">
		body {
			background-color: #dc322f;
			color: #fdf6e3;
		}
	
		pre {
			border: 1px dotted #fdf6e3;
			padding: 12px;
			margin: 8px;
		}
	</style>
</head>
<body>
	<h1>HTTP Panic: {{.RequestDescription}}</h1>
	<p>{{.RecoveredPanic}}</p>

	{{if .Stack}}
	<div class="stack">
		<h3>Runtime stack</h3>
		<pre>{{.StackAsString}}</pre>
	</div>
	{{end}}
</body>
</html>
`))
)

// ErrorPageData represents the data given to the error page template.
type ErrorPageData struct {
	Code    int
	Message string
}

// ErrorPage is an error page instance that will get rendered for the current
// request.
type ErrorPage struct {
	logger logrus.FieldLogger
	tpl    *template.Template
}

// NewErrorPage creates a new ErrorPage.
func NewErrorPage(logger logrus.FieldLogger, tpl *template.Template) *ErrorPage {
	return &ErrorPage{
		logger: logger,
		tpl:    tpl,
	}
}

// DefaultErrorPage creates the default error page.
func DefaultErrorPage(logger logrus.FieldLogger) *ErrorPage {
	return NewErrorPage(logger, errorPage)
}

// FormatPanicError formats a panic for Negroni.
func (p *ErrorPage) FormatPanicError(w http.ResponseWriter, r *http.Request, infos *negroni.PanicInformation) {
	logger := server.GetLoggerOrDefault(r, p.logger)
	Template(logger, w, p.tpl, ErrorPageData{
		Code:    http.StatusInternalServerError,
		Message: http.StatusText(http.StatusInternalServerError),
	}, http.StatusInternalServerError)

	logger.WithFields(logrus.Fields{
		"panic":  infos.RecoveredPanic,
		"method": infos.Request.Method,
		"url":    infos.Request.URL.String(),
		"stack":  infos.StackAsString(),
	}).Errorln("panic")
}

// RespondError writes the error page to the response writer.
func (p *ErrorPage) RespondError(w http.ResponseWriter, r *http.Request, code int, errorMessage string, fields logrus.Fields, err error) {
	logger := server.GetLoggerOrDefault(r, p.logger)
	Template(logger, w, p.tpl, ErrorPageData{
		Code:    code,
		Message: errorMessage,
	}, code)

	if logger != nil {
		if fields != nil {
			logger = logger.WithFields(fields)
		}
		if err != nil {
			logger = logger.WithError(err)
		}
		logger.Error(errorMessage)
	}
}

// Error displays the default error page.
func Error(w http.ResponseWriter, r *http.Request, code int, errorMessage string, fields logrus.Fields, err error) {
	getErrorPage(r).RespondError(w, r, code, errorMessage, fields, err)
}

func getErrorPage(r *http.Request) *ErrorPage {
	item := r.Context().Value(errorPageContextKey)
	if item != nil {
		if ep, ok := item.(*ErrorPage); ok {
			return ep
		}
	}

	return DefaultErrorPage(nil)
}

type panicFormatter struct {
	logger logrus.FieldLogger
}

// NewPanicFormatter creates a formatter to display panics.
func NewPanicFormatter(logger logrus.FieldLogger) negroni.PanicFormatter {
	return &panicFormatter{
		logger: logger,
	}
}

func (p *panicFormatter) FormatPanicError(w http.ResponseWriter, _ *http.Request, infos *negroni.PanicInformation) {
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	if err := panicPage.Execute(w, infos); err != nil {
		p.logger.WithError(err).Errorln("failed to render panic")
	}
}
