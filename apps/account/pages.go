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

package account

import (
	"bytes"
	"net/http"
	"text/template"
	"time"

	"github.com/julienschmidt/httprouter"
	uuid "github.com/satori/go.uuid"
	"github.com/sirupsen/logrus"
	"github.com/tamasd/simplesite/apps/token"
	"github.com/tamasd/simplesite/database"
	"github.com/tamasd/simplesite/form"
	"github.com/tamasd/simplesite/keyvalue"
	"github.com/tamasd/simplesite/mailer"
	"github.com/tamasd/simplesite/page"
	"github.com/tamasd/simplesite/respond"
	"github.com/tamasd/simplesite/server"
	"github.com/tamasd/simplesite/session"
	"github.com/urfave/negroni"
)

const (
	tokenCategoryRegistationVerification = "reg-verification"
)

var (
	registrationPage = page.SubPage(`
{{define "body"}}
<h1>Register</h1>
<form method="POST">
	{{.ErrorMessages}}
	{{.CSRFToken}}
	<p><label>Username: <br /><input type="textfield" name="Username" value="{{.Data.Username}}" /></label></p>
	<p><label>Email: <br /><input type="email" name="Email" value="{{.Data.Email}}" /></label></p>
	<p><label>Password: <br /><input type="password" name="Password" value="{{.Data.Password}}" /></label></p>
	<p><label>Accept TOS: <input type="checkbox" name="AcceptTOS" value="true" {{if .Data.AcceptTOS}}checked="checked"{{end}} /></label></p>
	<p><input type="submit" value="Register" /></p>
</form>
{{end}}
`)

	registrationMail = template.Must(template.New("regmail").Parse(
		"From: {{.From}}\r\n" +
			"To: {{.To}}\r\n" +
			"Subject: Registration validation\r\n" +
			"\r\n" +
			"{{.URL}}\r\n",
	))

	loginPage = page.SubPage(`
{{define "body"}}
<h1>Login</h1>
<form method="POST">
	{{.ErrorMessages}}
	{{.CSRFToken}}
	<p><label>Username: <br /><input type="textfield" name="Username" value="{{.Data.Username}}" /></label></p>
	<p><label>Password: <br /><input type="password" name="Password" value="{{.Data.Password}}" /></label></p>
	<p><input type="submit" value="Log in" /></p>
</form>
{{end}}
`)
)

type registrationPageFormData struct {
	Username  string
	Email     string
	Password  string
	AcceptTOS bool
}

type registrationMailData struct {
	From string
	To   string
	URL  string
}

type loginPageFormData struct {
	Username string
	Password string
}

// Pages returns the html pages for the Account entity.
func Pages(store keyvalue.Store, m *session.Middleware, passwordValidator PasswordValidator, mailer mailer.Mailer, baseurl *server.BaseURL) []server.Route {
	rf := NewRegistrationForm(passwordValidator, mailer, baseurl)
	anonmw := session.MustBeAnonymousMiddleware()
	txmw := database.NewTxMiddleware(true)

	r := []server.Route{
		LogoutPage(m),
		{http.MethodGet, "/verify/:uuid/:token", server.WrapF(rf.Verify, anonmw, txmw)},
	}
	r = append(r, form.NewForm(store, "Register", registrationPage, rf).Pages("/register", anonmw, txmw)...)
	r = append(r, form.NewForm(store, "Login", loginPage, NewLoginForm(m)).Pages("/login", anonmw, txmw)...)

	return r
}

type loginForm struct {
	AccessCheckLoader
	sessionMiddleware *session.Middleware
}

// NewLoginForm creates the delegate for the login form.
func NewLoginForm(m *session.Middleware) form.Delegate {
	return &loginForm{
		sessionMiddleware: m,
	}
}

func (f *loginForm) LoadData(_ *http.Request) (interface{}, error) {
	return &loginPageFormData{}, nil
}

func (f *loginForm) Validate(_ *http.Request, v interface{}) []string {
	var errs []string
	data := v.(*loginPageFormData)
	if data.Username == "" {
		errs = append(errs, "Username is required")
	}
	if data.Password == "" {
		errs = append(errs, "Password is required")
	}

	return errs
}

func (f *loginForm) Submit(w http.ResponseWriter, r *http.Request, v interface{}) form.FormSubmitResult {
	conn := database.Get(r)
	data := v.(*loginPageFormData)
	acc, err := LoadAccountByUsername(conn, data.Username)
	if err != nil {
		return form.Error("Login failed", err)
	}
	if !acc.Active {
		return form.Error("User is inactive", nil)
	}

	if !acc.CheckPassword(data.Password) {
		return form.Error("Invalid password", nil)
	}

	if err = f.sessionMiddleware.RegenerateSession(w, r, acc.ID); err != nil {
		return form.Error("Failed to regenerate session", nil)
	}

	return form.Redirect("")
}

type registrationForm struct {
	AccessCheckLoader
	passwordValidator PasswordValidator
	mailer            mailer.Mailer
	baseurl           *server.BaseURL
}

// RegistrationFormDelegate expands the form.Delegate with a registration
// verification endpoint.
type RegistrationFormDelegate interface {
	form.Delegate
	Verify(w http.ResponseWriter, r *http.Request)
}

// NewRegistrationForm creates the delegate for the registration form.
func NewRegistrationForm(passwordValidator PasswordValidator, mailer mailer.Mailer, baseurl *server.BaseURL) RegistrationFormDelegate {
	return &registrationForm{
		passwordValidator: passwordValidator,
		mailer:            mailer,
		baseurl:           baseurl,
	}
}

func (f *registrationForm) LoadData(_ *http.Request) (interface{}, error) {
	return &registrationPageFormData{}, nil
}

func (f *registrationForm) Validate(_ *http.Request, v interface{}) []string {
	var errs []string
	data := v.(*registrationPageFormData)
	if data.Username == "" {
		errs = append(errs, "Username is required")
	} else if IsAccountnameBlacklisted(data.Username) {
		errs = append(errs, "Username is blacklisted")
	}
	if data.Email == "" {
		errs = append(errs, "Email is required")
	}
	if data.Password == "" {
		errs = append(errs, "Password is required")
	} else {
		comp, err := f.passwordValidator.Validate(data.Password)
		if err != nil {
			errs = append(errs, "Error validating password")
		} else {
			if comp {
				errs = append(errs, "This password is found in a previous data breach")
			}
		}
	}
	if !data.AcceptTOS {
		errs = append(errs, "TOS must be accepted")
	}

	return errs
}

func (f *registrationForm) Submit(_ http.ResponseWriter, r *http.Request, v interface{}) form.FormSubmitResult {
	data := v.(*registrationPageFormData)
	logger := server.GetLogger(r)
	conn := database.Get(r)

	a := &Account{
		Username: data.Username,
		Email:    data.Email,
	}
	a.SetPassword(data.Password)

	if err := a.Save(conn); err != nil {
		return form.Error("Account already exists", err)
	}

	tokenManager := token.NewTokenFromRequest(r)

	expires := time.Now().Add(24 * time.Hour)
	t, err := tokenManager.Create(a.ID, tokenCategoryRegistationVerification, &expires)
	if err != nil {
		return form.Error("Failed to create account", err)
	}

	buf := bytes.NewBuffer(nil)
	if err = registrationMail.Execute(buf, registrationMailData{
		From: f.mailer.From(),
		To:   a.Email,
		URL:  f.baseurl.Path("/verify/", a.ID.String(), t),
	}); err != nil {
		return form.Error("Failed to create email", err)
	}
	body := buf.Bytes()

	logger.WithFields(logrus.Fields{
		"to":   a.Email,
		"body": string(body),
	}).Traceln("sending registration verification mail")

	if err := f.mailer.Send([]string{a.Email}, body); err != nil {
		return form.Error("Failed to send email", err)
	}

	return form.Redirect("")
}

// Verify is the handler for the registration verification endpoint.
func (f *registrationForm) Verify(w http.ResponseWriter, r *http.Request) {
	p := httprouter.ParamsFromContext(r.Context())
	idstr := p.ByName("uuid")
	tok := p.ByName("token")
	logger := server.GetLogger(r)
	conn := database.Get(r)

	id, err := uuid.FromString(idstr)
	if err != nil {
		logger.WithError(err).Debugln("failed to parse uuid")
		respond.Error(w, r, http.StatusNotFound, "", nil, nil)
		return
	}

	tokenManager := token.NewTokenFromRequest(r)

	consumed, err := tokenManager.Consume(id, tokenCategoryRegistationVerification, tok)
	if err != nil {
		respond.Error(w, r, http.StatusInternalServerError, "failed to consume token", nil, err)
		return
	}
	if !consumed {
		respond.Error(w, r, http.StatusNotFound, "token not found", nil, nil)
		return
	}

	acc, err := loadAccountByCondition(conn, "id = $1 AND active = $2", id, false)
	if err != nil {
		respond.Error(w, r, http.StatusInternalServerError, "account loading error", nil, err)
		return
	}

	acc.Active = true
	if err = acc.Save(conn); err != nil {
		respond.Error(w, r, http.StatusInternalServerError, "account saving error", nil, err)
		return
	}

	http.Redirect(w, r, "/", http.StatusFound)
}

// LogoutPage is the handler for the logout page.
func LogoutPage(m *session.Middleware, middlewares ...negroni.Handler) server.Route {
	return server.Route{
		Method: http.MethodGet,
		Path:   "/logout",
		Handler: server.WrapF(func(w http.ResponseWriter, r *http.Request) {
			m.DeleteSession(w, r)
			http.Redirect(w, r, "/", http.StatusFound)
		}, append([]negroni.Handler{session.MustBeLoggedInMiddleware(), session.CSRFTokenMiddleware()}, middlewares...)...),
	}
}

// PasswordValidator checks if a password is valid (strong enough, not
// compromised) when users register or change password.
type PasswordValidator interface {
	Validate(pw string) (bool, error)
}

// PasswordValidatorFunc is a single function implementation of
// PasswordValidator.
type PasswordValidatorFunc func(pw string) (bool, error)

func (f PasswordValidatorFunc) Validate(pw string) (bool, error) {
	return f(pw)
}
