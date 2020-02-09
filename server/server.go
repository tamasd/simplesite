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

package server

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/sirupsen/logrus"
	"github.com/tamasd/simplesite/util"
	"github.com/urfave/negroni"
	"golang.org/x/crypto/acme/autocert"
)

const (
	loggerContextKey = "logger"
)

// GetLogger returns the logger from the request context.
func GetLogger(r *http.Request) logrus.FieldLogger {
	return r.Context().Value(loggerContextKey).(logrus.FieldLogger)
}

// GetLoggerOrDefault returns the logger from the request context, or the given
// default one if it is not found.
func GetLoggerOrDefault(r *http.Request, l logrus.FieldLogger) logrus.FieldLogger {
	item := r.Context().Value(loggerContextKey)
	if item != nil {
		if logger, ok := item.(logrus.FieldLogger); ok {
			return logger
		}
	}

	return l
}

// Route represents a set of method, path and http.Handler.
type Route struct {
	Method  string
	Path    string
	Handler http.Handler
}

// Server is the main application server.
type Server struct {
	addr string

	router     *Router
	middleware *negroni.Negroni
	logger     logrus.FieldLogger

	HTTPS struct {
		LetsEncrypt struct {
			Directory string
			WhiteList []string
		}
		Certificate struct {
			Certfile string
			Keyfile  string
		}
	}
}

// New creates a new server.
func New(logger logrus.FieldLogger, addr string, panicFormatter negroni.PanicFormatter) *Server {
	s := &Server{
		addr:   addr,
		logger: logger,

		router:     NewRouter(),
		middleware: negroni.New(),
	}

	recovery := negroni.NewRecovery()
	recovery.Logger = logger
	recovery.Formatter = panicFormatter
	s.middleware.Use(recovery)
	s.middleware.UseFunc(s.profiler)

	return s
}

func (s *Server) profiler(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	start := time.Now()

	l := s.logger.WithFields(logrus.Fields{
		"reqid":  util.RandomHexString(16),
		"method": r.Method,
		"path":   r.URL.Path,
		"host":   r.Host,
	})

	w.Header().Set("Server", "Unknown")
	r = r.WithContext(context.WithValue(r.Context(), loggerContextKey, l))

	next(w, r)

	status := w.(negroni.ResponseWriter).Status()
	l.WithFields(logrus.Fields{
		"status-code": status,
		"status":      http.StatusText(status),
		"latency":     time.Since(start),
	}).Infoln("completed handling request")
}

// Router returns the server's router.
func (s *Server) Router() *Router {
	return s.router
}

// CreateHTTPServer creates a http.Server from the application server.
//
// This server is fully configured and it is ready to be started.
func (s *Server) CreateHTTPServer() *http.Server {
	s.middleware.UseHandler(s.router.Handler())

	srv := &http.Server{
		Addr:    s.addr,
		Handler: s.middleware,
	}

	srv.TLSConfig = &tls.Config{
		PreferServerCipherSuites: true,
		CurvePreferences: []tls.CurveID{
			tls.CurveP521,
			tls.CurveP384,
			tls.CurveP256,
			tls.X25519,
		},
		MinVersion: tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		},
	}

	return srv
}

// Start starts an http server that is created from the application server.
func (s *Server) Start() error {
	srv := s.CreateHTTPServer()

	if s.HTTPS.LetsEncrypt.Directory != "" {
		m := autocert.Manager{
			Cache:      autocert.DirCache(s.HTTPS.LetsEncrypt.Directory),
			HostPolicy: autocert.HostWhitelist(s.HTTPS.LetsEncrypt.WhiteList...),
		}
		srv.TLSConfig.GetCertificate = m.GetCertificate

		return srv.ListenAndServeTLS("", "")
	}

	if s.HTTPS.Certificate.Certfile != "" && s.HTTPS.Certificate.Keyfile != "" {
		return srv.ListenAndServeTLS(s.HTTPS.Certificate.Certfile, s.HTTPS.Certificate.Keyfile)
	}

	return srv.ListenAndServe()
}

// IsHTTPS tells if the server is configured to use HTTPS.
func (s *Server) IsHTTPS() bool {
	return s.HTTPS.LetsEncrypt.Directory != "" || (s.HTTPS.Certificate.Certfile != "" && s.HTTPS.Certificate.Keyfile != "")
}

// Use adds middlewares to the end of the server's middleware chain.
func (s *Server) Use(middlewares ...negroni.Handler) {
	for _, m := range middlewares {
		s.middleware.Use(m)
	}
}

// Router represents an abstract http router.
type Router struct {
	router *httprouter.Router
}

// NewRouter creates a router.
func NewRouter() *Router {
	router := httprouter.New()
	router.RedirectTrailingSlash = true
	router.RedirectFixedPath = true
	router.HandleMethodNotAllowed = true
	router.HandleOPTIONS = true

	return &Router{
		router: router,
	}
}

// Handler returns the underlying http.Handler of the router.
func (r *Router) Handler() http.Handler {
	return r.router
}

// Handle adds a handler to the router.
func (r *Router) Handle(method, path string, handler http.Handler) *Router {
	r.router.Handler(method, path, handler)
	return r
}

// Add adds routes to the router.
func (r *Router) Add(routes ...Route) *Router {
	for _, route := range routes {
		r.Handle(route.Method, route.Path, route.Handler)
	}

	return r
}

func (r *Router) Get(path string, handler http.Handler) *Router {
	return r.Handle(http.MethodGet, path, handler)
}

func (r *Router) GetF(path string, handler http.HandlerFunc) *Router {
	return r.Handle(http.MethodGet, path, handler)
}

func (r *Router) Head(path string, handler http.Handler) *Router {
	return r.Handle(http.MethodHead, path, handler)
}

func (r *Router) HeadF(path string, handler http.HandlerFunc) *Router {
	return r.Handle(http.MethodHead, path, handler)
}

func (r *Router) Post(path string, handler http.Handler) *Router {
	return r.Handle(http.MethodPost, path, handler)
}

func (r *Router) PostF(path string, handler http.HandlerFunc) *Router {
	return r.Handle(http.MethodPost, path, handler)
}

func (r *Router) Put(path string, handler http.Handler) *Router {
	return r.Handle(http.MethodPut, path, handler)
}

func (r *Router) PutF(path string, handler http.HandlerFunc) *Router {
	return r.Handle(http.MethodPut, path, handler)
}

func (r *Router) Patch(path string, handler http.Handler) *Router {
	return r.Handle(http.MethodPatch, path, handler)
}

func (r *Router) PatchF(path string, handler http.HandlerFunc) *Router {
	return r.Handle(http.MethodPatch, path, handler)
}

func (r *Router) Delete(path string, handler http.Handler) *Router {
	return r.Handle(http.MethodDelete, path, handler)
}

func (r *Router) DeleteF(path string, handler http.HandlerFunc) *Router {
	return r.Handle(http.MethodDelete, path, handler)
}

// Wrap wraps a http handler with middlewares.
func Wrap(h http.Handler, middlewares ...negroni.Handler) http.Handler {
	if len(middlewares) == 0 {
		return h
	}

	n := negroni.New(middlewares...)
	n.UseHandler(h)

	return n
}

// WrapF wraps a http handler function with middlewares.
func WrapF(h http.HandlerFunc, middlewares ...negroni.Handler) http.Handler {
	return Wrap(h, middlewares...)
}

// PrefixRoutes adds prefixes to the routes.
func PrefixRoutes(prefix string, routes []Route) []Route {
	ret := make([]Route, len(routes))
	for i, route := range routes {
		route.Path = prefix + route.Path
		ret[i] = route
	}

	return ret
}

// BaseURL represents the server's base url.
type BaseURL struct {
	base url.URL
}

// ParseBaseURL creates a new BaseURL by parsing the string form.
func ParseBaseURL(rawurl string) (*BaseURL, error) {
	u, err := url.Parse(rawurl)
	if err != nil {
		return nil, err
	}

	return &BaseURL{
		base: *u,
	}, nil
}

// Path creates a new url from the base url by appending items to its path.
func (b *BaseURL) Path(parts ...string) string {
	base := b.base
	base.Path = path.Join(append([]string{base.Path}, parts...)...)

	return base.String()
}
