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

package testutil

import (
	"bytes"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-redis/redis/v7"
	uuid "github.com/satori/go.uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"github.com/tamasd/simplesite/config"
	"github.com/tamasd/simplesite/database"
	"github.com/tamasd/simplesite/keyvalue"
	"github.com/tamasd/simplesite/mailer"
	"github.com/tamasd/simplesite/server"
	"github.com/tamasd/simplesite/session"
	"github.com/tamasd/simplesite/site"
	"github.com/tamasd/simplesite/util"
	"golang.org/x/net/publicsuffix"
)

const (
	redisDeleteBatchSize = 32
	baseurl              = "http://example.com"
	testEmail            = "test@example.com"
)

var (
	dbnameRegexp          = regexp.MustCompile(`(dbname=)(\S+)`)
	verificationLinkRegex = regexp.MustCompile(`(https?:[a-zA-Z0-9/.-]+)`)
)

// TestRegData creates a random data set for a filled-in registration form.
func TestRegData() *url.Values {
	regdata := &url.Values{}
	regdata.Set("Username", util.RandomHexString(16))
	regdata.Set("Email", testEmail)
	regdata.Set("Password", util.RandomHexString(32))
	regdata.Set("AcceptTOS", "true")

	return regdata
}

func extractVerificationLink(data []byte) string {
	return verificationLinkRegex.FindString(string(data))
}

type testLogger struct {
	logrus.FieldLogger
	buf *bytes.Buffer
}

// TestLogger creates a logger that saves its data instead of outputting it to
// the stdout.
func TestLogger() logrus.FieldLogger {
	buf := bytes.NewBuffer(nil)
	logger := logrus.New()
	logger.Out = buf

	return &testLogger{
		FieldLogger: logger,
		buf:         buf,
	}
}

// GetLog returns the contents of a logger.
//
// This function must be only used with the direct output of TestLogger().
func GetLog(logger logrus.FieldLogger) string {
	return logger.(*testLogger).buf.String()
}

// RedisDeletePattern deletes items from the redis database that match the given
// pattern.
func RedisDeletePattern(client *redis.Client, pattern string) error {
	var keys []string
	var cursor uint64
	var err error
	p := client.Pipeline()
	for {
		keys, cursor, err = client.Scan(cursor, pattern, redisDeleteBatchSize).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			p.Del(keys...)
		}

		if cursor == 0 {
			break
		}
	}
	_, err = p.Exec()
	return err
}

// Message represents a sent email.
type Message struct {
	To      []string
	Message []byte
}

// TestMailer is an in-memory implementation of the Mailer interface.
type TestMailer struct {
	Messages []Message
}

func (m *TestMailer) From() string {
	return "test@example.com"
}

func (m *TestMailer) Send(to []string, msg []byte) error {
	m.Messages = append(m.Messages, Message{
		To:      to,
		Message: msg,
	})

	return nil
}

// SetupTestDatabase creates a database on the database server.
//
// It returns the connection url of the new database and a function that will
// delete the database when it is run.
func SetupTestDatabase(dburl string) (string, func()) {
	testdb := "test_" + util.RandomHexString(8)
	conn, err := database.Connect(dburl)
	Must(err)
	_, err = conn.Exec("CREATE DATABASE " + testdb)
	Must(err)

	testdburl := dbnameRegexp.ReplaceAllString(dburl, `${1}`+testdb)

	return testdburl, func() {
		_, err = conn.Exec(`
			SELECT pg_terminate_backend(pg_stat_activity.pid)
			FROM pg_stat_activity
			WHERE pg_stat_activity.datname = $1
				AND pid <> pg_backend_pid()
		`, testdb)
		Must(err)
		_, err = conn.Exec("DROP DATABASE " + testdb)
		Must(err)
	}
}

func NewTestMailer() *TestMailer {
	return &TestMailer{}
}

// SetupTestSiteFromEnv creates a test site from environment variables.
func SetupTestSiteFromEnv() *TestSite {
	return SetupTestSite(
		os.Getenv("TEST_DB"),
		os.Getenv("TEST_REDIS"),
	)
}

// SetupTestSite creates a test site.
func SetupTestSite(dburl, redisurl string) *TestSite {
	redisPrefix := util.RandomHexString(8) + ":"
	testdb, dbcleanup := SetupTestDatabase(dburl)

	cfg := config.MapStorage{
		"log_level":    "trace",
		"redis":        redisurl,
		"redis_prefix": redisPrefix,
		"baseurl":      baseurl,
		"db":           testdb,
	}
	s := site.NewSite(cfg)
	logger := TestLogger()
	mail := NewTestMailer()

	return &TestSite{
		Server: s.CreateServer(logger, func() (mailer.Mailer, error) {
			return mail, nil
		}),
		Mailer:      mail,
		testdb:      testdb,
		dbcleanup:   dbcleanup,
		redisurl:    redisurl,
		redisPrefix: redisPrefix,
	}
}

// TestSite represents a version of *site.Site that is meant to be used for
// general integration testing.
type TestSite struct {
	Server      *server.Server
	Mailer      *TestMailer
	testdb      string
	dbcleanup   func()
	redisurl    string
	redisPrefix string
}

func (ts *TestSite) Database() database.DB {
	conn, err := database.Connect(ts.testdb)
	Must(err)
	return conn
}

func (ts *TestSite) KeyValueStore() keyvalue.Store {
	return keyvalue.NewPrefixed(keyvalue.NewRedis(redis.NewClient(&redis.Options{
		Addr: ts.redisurl,
	})), ts.redisPrefix)
}

// Cleanup cleans the database and redis.
//
// This function is meant to be deferred after CreateTestSite is called.
func (ts *TestSite) Cleanup() {
	rc := redis.NewClient(&redis.Options{
		Addr: ts.redisurl,
	})

	Must(RedisDeletePattern(rc, ts.redisPrefix+"*"))
	ts.dbcleanup()
}

// CreateClient creates a mock http client for the test site.
//
// This client has its own separate cookie storage.
func (ts *TestSite) CreateClient(t *testing.T) *TestClient {
	return newTestClient(t, ts)
}

// Must panics if err is not nil.
func Must(err error) {
	if err != nil {
		panic(err)
	}
}

// TestClient is a mock http client, meant to be used in integration testing.
type TestClient struct {
	server       *http.Server
	jar          http.CookieJar
	t            *testing.T
	Page         *goquery.Document
	LastRequest  *http.Request
	LastResponse *http.Response
	baseurl      url.URL
	testSite     *TestSite
}

func newTestClient(t *testing.T, ts *TestSite) *TestClient {
	jar, err := cookiejar.New(&cookiejar.Options{
		PublicSuffixList: publicsuffix.List,
	})
	Must(err)

	bu, _ := url.Parse(baseurl)

	return &TestClient{
		server:   ts.Server.CreateHTTPServer(),
		jar:      jar,
		t:        t,
		baseurl:  *bu,
		testSite: ts,
	}
}

// CurrentUID returns the current user's id.
func (c *TestClient) CurrentUID() uuid.UUID {
	u, _ := url.Parse(baseurl)
	cookies := c.jar.Cookies(u)

	for _, cookie := range cookies {
		if cookie.Name != session.SessionCookieName {
			continue
		}

		uid, err := uuid.FromString(strings.Split(cookie.Value, ":")[0])
		require.Nil(c.t, err)

		return uid
	}

	return uuid.Nil
}

// Request emulates sending a request to the test site.
func (c *TestClient) Request(method, target string, body io.Reader, alter ...func(*http.Request)) *http.Response {
	r := httptest.NewRequest(method, c.absoluteTarget(target), body)

	for _, cookie := range c.jar.Cookies(r.URL) {
		r.AddCookie(cookie)
	}

	for _, f := range alter {
		f(r)
	}

	c.LastRequest = r

	rr := httptest.NewRecorder()
	c.server.Handler.ServeHTTP(rr, r)
	resp := rr.Result()

	c.jar.SetCookies(r.URL, resp.Cookies())

	if resp.Header.Get("Content-Type") == "text/html; charset=utf-8" {
		defer func() { _ = resp.Body.Close() }()
		var err error
		c.Page, err = goquery.NewDocumentFromReader(resp.Body)
		require.Nil(c.t, err)
	} else {
		c.Page = nil
	}

	c.LastResponse = resp

	return resp
}

// FormValues parses the existing form values on a form page.
//
// This is useful for edit forms, or testing form submission errors.
func (c *TestClient) FormValues(formid string) *url.Values {
	v := &url.Values{}

	root := c.Page.Find(formSelector(formid)).First()

	root.Find("select").Each(func(_ int, sel *goquery.Selection) {
		name := sel.AttrOr("name", "")
		if name == "" {
			return
		}
		multiple := sel.AttrOr("multiple", "") == "multiple"

		sel.Find(`option[selected="selected"]`).Each(func(_ int, opt *goquery.Selection) {
			val := opt.AttrOr("value", "")
			if multiple {
				v.Add(name, val)
			} else {
				v.Set(name, val)
			}
		})
	})

	root.Find("textarea").Each(func(_ int, textarea *goquery.Selection) {
		name := textarea.AttrOr("name", "")
		if name == "" {
			return
		}

		v.Set(name, textarea.Text())
	})

	root.Find("input").Each(func(_ int, input *goquery.Selection) {
		name := input.AttrOr("name", "")
		if name == "" {
			return
		}
		val := input.AttrOr("value", "")

		switch input.AttrOr("type", "") {
		case "button", "submit", "image", "reset":
			return
		case "file":
			// TODO support file type input?
			return
		case "checkbox":
			if input.AttrOr("checked", "") == "checked" {
				v.Set(name, val)
			}
		case "radio":
			if input.AttrOr("checked", "") == "checked" {
				v.Add(name, val)
			}
		default:
			v.Set(name, val)
		}
	})

	return v
}

// ClickLink emulates clicking on a link.
func (c *TestClient) ClickLink(selector string, alter ...func(*http.Request)) *http.Response {
	href := c.Page.Find(selector).AttrOr("href", "")
	require.NotZero(c.t, href)
	return c.Request(http.MethodGet, href, nil, alter...)
}

func (c *TestClient) absoluteTarget(target string) string {
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		return target
	}

	bu := c.baseurl

	targeturl, err := url.Parse(target)
	require.Nil(c.t, err)

	bu.Path = path.Join(bu.Path, targeturl.Path)
	bu.RawQuery = targeturl.RawQuery

	return bu.String()
}

// FollowRedirect follows a redirect if applicable.
func (c *TestClient) FollowRedirect(alter ...func(*http.Request)) *http.Response {
	require.Contains(c.t, []int{
		http.StatusMultipleChoices,
		http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusSeeOther,
		http.StatusNotModified,
		http.StatusUseProxy,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect,
	}, c.LastResponse.StatusCode)

	location := c.LastResponse.Header.Get("Location")
	require.NotZero(c.t, location)

	return c.Request(http.MethodGet, location, nil, alter...)
}

// Form requests a form page, and parses the necessary data so it can be
// submitted easily.
func (c *TestClient) Form(url string, alter ...func(*http.Request)) SubmittableForm {
	resp := c.Request(http.MethodGet, url, nil, alter...)
	require.Equal(c.t, http.StatusOK, resp.StatusCode)
	require.Equal(c.t, "text/html; charset=utf-8", resp.Header.Get("Content-Type"))

	formid := c.Page.Find("input[name=FormID]").AttrOr("value", "")
	formtoken := c.Page.Find("input[name=FormToken]").AttrOr("value", "")
	require.NotZero(c.t, formid)
	require.NotZero(c.t, formtoken)

	return &submittableForm{
		TestClient: c,
		url:        url,
		formid:     formid,
		formtoken:  formtoken,
	}
}

// RegistrationAndLogin emulates a registration and a login of an account.
func (c *TestClient) RegistrationAndLogin(regdata *url.Values) {
	resp := c.Form("/register").Submit(regdata)
	require.Equal(c.t, http.StatusFound, resp.StatusCode)

	require.Len(c.t, c.testSite.Mailer.Messages, 1)

	verificationLink := extractVerificationLink(c.testSite.Mailer.Messages[0].Message)
	resp = c.Request(http.MethodGet, verificationLink, nil)
	require.Equal(c.t, http.StatusFound, resp.StatusCode)

	logindata := &url.Values{}
	logindata.Set("Username", regdata.Get("Username"))
	logindata.Set("Password", regdata.Get("Password"))
	resp = c.Form("/login").Submit(logindata)
	require.Equal(c.t, http.StatusFound, resp.StatusCode)
}

// SubmittableForm represents a form that is ready to be submitted with the
// given values.
type SubmittableForm interface {
	Submit(postValues *url.Values, alter ...func(*http.Request)) *http.Response
}

type submittableForm struct {
	*TestClient
	url       string
	formid    string
	formtoken string
}

func (sf *submittableForm) Submit(postValues *url.Values, alter ...func(*http.Request)) *http.Response {
	if postValues.Get("FormID") == "" {
		postValues.Set("FormID", sf.formid)
	}
	if postValues.Get("FormToken") == "" {
		postValues.Set("FormToken", sf.formtoken)
	}

	return sf.Request(http.MethodPost, sf.url, bytes.NewBufferString(postValues.Encode()),
		append([]func(*http.Request){
			func(r *http.Request) {
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			},
		}, alter...)...,
	)
}

func formSelector(formid string) string {
	if formid == "" {
		return "form"
	}

	return "form#" + formid
}
