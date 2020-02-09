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

package site

import (
	"net/smtp"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/go-redis/redis/v7"
	hibp "github.com/mattevans/pwned-passwords"
	"github.com/sirupsen/logrus"
	"github.com/tamasd/simplesite/apps/account"
	"github.com/tamasd/simplesite/apps/file"
	"github.com/tamasd/simplesite/apps/frontpage"
	"github.com/tamasd/simplesite/apps/post"
	"github.com/tamasd/simplesite/apps/token"
	"github.com/tamasd/simplesite/config"
	"github.com/tamasd/simplesite/database"
	"github.com/tamasd/simplesite/keyvalue"
	"github.com/tamasd/simplesite/mailer"
	"github.com/tamasd/simplesite/respond"
	"github.com/tamasd/simplesite/server"
	"github.com/tamasd/simplesite/session"
	"github.com/tamasd/simplesite/util"
)

var (
	loggerOut      = os.Stdout
	loggerExitFunc = os.Exit
)

// Site is the main package of this website.
type Site struct {
	config config.Storage
}

// NewSite creates a new site from the given configuration.
func NewSite(config config.Storage) *Site {
	return &Site{
		config: config,
	}
}

// Logger creates the configured logger for the site.
func (s *Site) Logger() logrus.FieldLogger {
	logger := logrus.New()
	logger.Out = loggerOut
	logger.ExitFunc = loggerExitFunc

	if level := s.config.Get("log_level"); level != "" {
		lvl, err := logrus.ParseLevel(level)
		if err != nil {
			logger.WithError(err).Fatalln("failed to parse log level")
			return nil
		}
		logger.SetLevel(lvl)
	}

	switch s.config.Get("log_format") {
	case "json":
		logger.Formatter = &logrus.JSONFormatter{}
	}

	hostname, _ := os.Hostname()
	return logger.WithField("hostname", hostname)
}

func (s *Site) server(logger logrus.FieldLogger) *server.Server {
	host := os.Getenv("HOST")
	port := os.Getenv("PORT")

	srv := server.New(logger, host+":"+port, respond.NewPanicFormatter(logger))
	srv.HTTPS.LetsEncrypt.Directory = s.config.Get("letsencrypt")
	srv.HTTPS.LetsEncrypt.WhiteList = strings.Fields(s.config.Get("letsencrypt_whitelist"))
	srv.HTTPS.Certificate.Certfile = s.config.Get("certfile")
	srv.HTTPS.Certificate.Keyfile = s.config.Get("keyfile")

	return srv
}

func (s *Site) redisClient() *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr: s.config.Get("redis"),
	})
}

func (s *Site) kvstore() keyvalue.Store {
	prefix := s.config.Get("redis_prefix")
	var store keyvalue.Store = keyvalue.NewRedis(s.redisClient())

	if prefix != "" {
		store = keyvalue.NewPrefixed(store, prefix)
	}

	return store
}

func (s *Site) smtpMailer() (mailer.Mailer, error) {
	smtpAddr := s.config.Get("smtp_addr")
	var auth smtp.Auth

	username := s.config.Get("smtp_username")
	password := s.config.Get("smtp_password")
	if username != "" && password != "" {
		auth = smtp.PlainAuth(
			"",
			username,
			password,
			strings.Split(smtpAddr, ":")[0],
		)
	}

	return mailer.NewSMTP(
		s.config.Get("smtp_from"),
		smtpAddr,
		auth,
	), nil
}

func (s *Site) baseURL() (*server.BaseURL, error) {
	return server.ParseBaseURL(s.config.Get("baseurl"))
}

// CreateServer creates the server instance with all middlewares and pages.
func (s *Site) CreateServer(logger logrus.FieldLogger, mailerFactory func() (mailer.Mailer, error)) *server.Server {
	kvstore := s.kvstore()
	formTokenStore := keyvalue.NewPrefixed(kvstore, "form:")
	pwned := hibp.NewClient(time.Hour)

	mail, err := mailerFactory()
	if err != nil {
		logger.WithError(err).Fatalln("failed to initialize smtp")
		return nil
	}

	baseurl, err := s.baseURL()
	if err != nil {
		logger.WithError(err).Fatalln("failed to parse base url")
		return nil
	}

	srv := s.server(logger)

	conn, err := database.Connect(s.config.Get("db"))
	if err != nil {
		logger.WithError(err).Fatalln("failed to connect to database")
		return nil
	}

	for _, e := range []database.DatabaseEntity{
		token.Token{},
		account.Account{},
		account.Permission{},
		post.Post{},
		post.PostRevision{},
	} {
		if err = database.Ensure(logger, conn, e); err != nil {
			logger.
				WithError(err).
				WithField("entity", reflect.TypeOf(e).Name()).
				Fatalln("failed to register entity")
			return nil
		}
	}

	sess := session.NewMiddleware(logger, keyvalue.NewPrefixed(kvstore, "session:"))
	dbmw := database.NewMiddleware(database.NewLoggerDB(logger, conn))

	srv.Use(sess, dbmw, account.PreloadPermissions())

	srv.Router().
		Add(file.AssetDir()).
		Add(file.MiscDir(logger)...).
		Add(frontpage.Page()).
		Add(account.Pages(formTokenStore, sess, account.PasswordValidatorFunc(pwned.Pwned.Compromised), mail, baseurl)...).
		Add(post.Pages(formTokenStore, util.NewFilter(logger).Filter)...)

	logger.Infoln("Starting server")

	//srv.Router().
	//	GetF("/debug/pprof", pprof.Index).
	//	GetF("/debug/pprof/cmdline", pprof.Cmdline).
	//	GetF("/debug/pprof/profile", pprof.Profile).
	//	GetF("/debug/pprof/symbol", pprof.Symbol).
	//	GetF("/debug/pprof/trace", pprof.Trace)

	return srv
}

// Start starts the site.
func (s *Site) Start() {
	logger := s.Logger()
	srv := s.CreateServer(logger, s.smtpMailer)
	if err := srv.Start(); err != nil {
		logger.WithError(err).Fatalln("server error")
		return
	}
}
