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
	"encoding/json"
	"html/template"
	"net/http"

	"github.com/sirupsen/logrus"
	"github.com/tamasd/simplesite/page"
	"github.com/tamasd/simplesite/util"
)

const (
	cspNonceLength = 16
)

// SessionInfo stores important information about the session.
type SessionInfo interface {
	GetCSRFToken() string
	LoggedIn() bool
}

// JSON formats a JSON response.
func JSON(l logrus.FieldLogger, w http.ResponseWriter, v interface{}, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)

	l = l.WithFields(logrus.Fields{
		"data":        v,
		"status-code": code,
		"status":      http.StatusText(code),
	})

	if data, err := json.Marshal(v); err != nil {
		l.Warnln("failed to serialize JSON data")
	} else if _, err = w.Write(data); err != nil {
		l.Warnln("failed to send JSON data")
	}
}

// Page formats a page-type response.
//
// A page-type response is supposed to be a subpage (see the page package), and
// it sets strict CSP.
func Page(l logrus.FieldLogger, w http.ResponseWriter, tpl *template.Template, title string, sess SessionInfo, access page.AccessChecker, bodyData interface{}) {
	nonce := util.RandomHexString(cspNonceLength)
	csp := `default-src 'none'; script-src 'self' 'nonce-` + nonce + `'; connect-src 'self'; img-src data: blob: 'self'; style-src 'self'; font-src 'self';`
	w.Header().Set("Content-Security-Policy", csp)
	Template(l, w, tpl, page.Data{
		Title:     title,
		Nonce:     nonce,
		CSRFToken: sess.GetCSRFToken(),
		LoggedIn:  sess.LoggedIn(),
		Access:    access,
		Body:      bodyData,
	}, http.StatusOK)
}

// Template renders a html template.
func Template(l logrus.FieldLogger, w http.ResponseWriter, tpl *template.Template, data interface{}, code int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-XSS-Protection", "1; mode=block")
	w.WriteHeader(code)
	if err := tpl.Execute(w, data); err != nil && l != nil {
		l.WithFields(logrus.Fields{
			"data":        data,
			"status-code": code,
			"status":      http.StatusText(code),
			"template":    tpl.Name(),
		}).WithError(err).Warnln("failed to render template")
	}
}
