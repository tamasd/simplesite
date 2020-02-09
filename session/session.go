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

package session

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	uuid "github.com/satori/go.uuid"
	"github.com/sirupsen/logrus"
	"github.com/tamasd/simplesite/keyvalue"
	"github.com/tamasd/simplesite/respond"
	"github.com/tamasd/simplesite/server"
	"github.com/tamasd/simplesite/util"
	"github.com/urfave/negroni"
)

const (
	// SessionCookieName is the name of the session cookie.
	SessionCookieName = "session"
)

const (
	sidLength  = 32
	csrfLength = 64
	sessionKey = "session"
	sidKey     = "sid"
)

var (
	sessionBufferPool = sync.Pool{New: func() interface{} {
		return bytes.NewBuffer(nil)
	}}
)

// Get returns the session from the current request context.
func Get(r *http.Request) *Session {
	return r.Context().Value(sessionKey).(*Session)
}

// GetSid returns the session id from the current request context.
func GetSid(r *http.Request) *string {
	return r.Context().Value(sidKey).(*string)
}

// Session represents the session that is saved to the key-value storage.
type Session struct {
	ID        uuid.UUID
	CSRFToken string
}

func (s *Session) GetCSRFToken() string {
	return s.CSRFToken
}

func (s *Session) LoggedIn() bool {
	return !uuid.Equal(s.ID, uuid.Nil)
}

func (s *Session) Read(p []byte) (int, error) {
	return len(p), json.Unmarshal(p, s)
}

func (s *Session) WriteTo(w io.Writer) (int64, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return 0, err
	}

	n, err := w.Write(b)

	return int64(n), err
}

// Middleware is the session middleware.
type Middleware struct {
	logger       logrus.FieldLogger
	store        keyvalue.Store
	SecureCookie bool
	CookieName   string
}

func NewMiddleware(logger logrus.FieldLogger, store keyvalue.Store) *Middleware {
	return &Middleware{
		logger:     logger,
		store:      store,
		CookieName: SessionCookieName,
	}
}

func (m *Middleware) ServeHTTP(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	sess := &Session{}
	sid := m.load(r, sess)
	if sid == "" {
		respond.Error(w, r, http.StatusInternalServerError, "session error", nil, nil)
		return
	}

	if sess.CSRFToken == "" {
		sess.CSRFToken = GenerateCSRFToken()
	}

	r = util.SetContext(r, sessionKey, sess)
	r = util.SetContext(r, sidKey, &sid)
	m.setSessionCookie(w, sid)

	next.ServeHTTP(w, r)

	buf := sessionBufferPool.Get().(*bytes.Buffer)
	defer func() {
		buf.Reset()
		sessionBufferPool.Put(buf)
	}()

	logger := server.GetLoggerOrDefault(r, m.logger)
	if _, err := sess.WriteTo(buf); err != nil {
		logger.WithError(err).Errorln("failed to encode session data")
		return
	}

	if sid != "" {
		if err := m.store.Set(sid, string(buf.Bytes())); err != nil {
			logger.WithError(err).Errorln("failed to save session")
			return
		}
	}
}

// RegenerateSession invalidates the previous session and creates a new one.
func (m *Middleware) RegenerateSession(w http.ResponseWriter, r *http.Request, id uuid.UUID) error {
	sid := GetSid(r)
	if err := m.store.Delete(*sid); err != nil {
		return err
	}
	*sid = GenerateSid(id)
	w.Header().Del("Set-Cookie")
	m.setSessionCookie(w, *sid)

	sess := Get(r)
	sess.ID = id
	sess.CSRFToken = GenerateCSRFToken()

	return nil
}

// DeleteSession removes the current session.
func (m *Middleware) DeleteSession(w http.ResponseWriter, r *http.Request) {
	logger := server.GetLogger(r)
	sid := GetSid(r)
	if err := m.store.Delete(*sid); err != nil {
		logger.WithError(err).Errorln("cannot delete session")
	}

	w.Header().Del("Set-Cookie")
	http.SetCookie(w, &http.Cookie{
		Name:     m.CookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   m.SecureCookie,
	})

	*sid = ""
}

func (m *Middleware) setSessionCookie(w http.ResponseWriter, sid string) {
	http.SetCookie(w, &http.Cookie{
		Name:     m.CookieName,
		Value:    sid,
		Path:     "/",
		Expires:  time.Now().AddDate(1, 0, 0),
		Secure:   m.SecureCookie,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

func (m *Middleware) load(r *http.Request, sess *Session) string {
	start := time.Now()
	l := server.GetLoggerOrDefault(r, m.logger)

	c, err := r.Cookie(m.CookieName)
	if err != nil {
		if err == http.ErrNoCookie {
			c = &http.Cookie{}
		} else {
			l.WithError(err).Warnln("failed to get cookie for session")
			return ""
		}
	}

	sid := c.Value
	if sid == "" {
		return GenerateSid(uuid.Nil)
	}

	sessdata, err := m.store.Get(sid)
	if err != nil {
		l.WithError(err).Warnln("failed to load session from store")
		return ""
	}

	if sessdata != "" {
		if _, err = sess.Read([]byte(sessdata)); err != nil {
			l.WithError(err).Warnln("failed to decode session data")
			return ""
		}
	}

	l.WithFields(logrus.Fields{
		"duration": time.Since(start),
	}).Traceln("successfully loaded session")

	return sid
}

// GenerateSid generates a new session id.
//
// The session id gets prefixed with the user id, so sessions can be invalidated
// for an user in case an account gets blocked or deleted.
func GenerateSid(id uuid.UUID) string {
	return id.String() + ":" + util.RandomHexString(sidLength)
}

// GenerateCSRFToken generates a new csrf token.
func GenerateCSRFToken() string {
	return util.RandomHexString(csrfLength)
}

// MustBeLoggedInMiddleware only lets the request proceed if an account is
// logged in.
func MustBeLoggedInMiddleware() negroni.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
		if sess := Get(r); sess.LoggedIn() {
			next.ServeHTTP(w, r)
		} else {
			respond.Error(w, r, http.StatusForbidden, "must be logged in", nil, nil)
		}
	}
}

// MustBeAnonymousMiddleware only lets the request proceed if the account is
// not logged in.
func MustBeAnonymousMiddleware() negroni.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
		if sess := Get(r); !sess.LoggedIn() {
			next.ServeHTTP(w, r)
		} else {
			respond.Error(w, r, http.StatusForbidden, "must not be logged in", nil, nil)
		}
	}
}

// CSRFTokenMiddleware enforces a CSRF token in the ?token= part of the URL.
func CSRFTokenMiddleware() negroni.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
		token := r.URL.Query().Get("token")
		if token == "" {
			respond.Error(w, r, http.StatusBadRequest, "missing csrf token", nil, nil)
			return
		}
		if sess := Get(r); token != sess.CSRFToken {
			respond.Error(w, r, http.StatusForbidden, "invalid csrf token", nil, nil)
			return
		}

		next.ServeHTTP(w, r)
	}
}
