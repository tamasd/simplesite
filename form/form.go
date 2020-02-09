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

package form

import (
	"errors"
	"html"
	"html/template"
	"net/http"
	"time"

	"github.com/monoculum/formam"
	"github.com/tamasd/simplesite/database"
	"github.com/tamasd/simplesite/keyvalue"
	"github.com/tamasd/simplesite/page"
	"github.com/tamasd/simplesite/respond"
	"github.com/tamasd/simplesite/server"
	"github.com/tamasd/simplesite/session"
	"github.com/tamasd/simplesite/util"
	"github.com/urfave/negroni"
)

const (
	multipartFormBuffer = 64 * 1024
	formIDLength        = 16
	formTokenLength     = 32
)

// Form has handlers for a standard HTML form.
type Form struct {
	store    keyvalue.Store
	title    string
	page     *template.Template
	delegate Delegate
}

// NewForm creates a new instance of Form.
//
// The key-value store will hold the form tokens. The page template is the
// surrounding page which will embed the form.
func NewForm(store keyvalue.Store, title string, page *template.Template, delegate Delegate) *Form {
	return &Form{
		store:    store,
		title:    title,
		page:     page,
		delegate: delegate,
	}
}

// Page is the main page that shows the form.
func (f *Form) Page(w http.ResponseWriter, r *http.Request) {
	sess := session.Get(r)
	data, err := f.delegate.LoadData(r)
	if err != nil {
		respond.Error(w, r, http.StatusNotFound, "not found", nil, err)
		return
	}
	fd := &FormPageData{
		Data: data,
	}
	fd.generateFormID()
	f.buildForm(w, r, sess, fd)
}

// Submit is the endpoint that handles the form submission.
func (f *Form) Submit(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(r); err != nil {
		respond.Error(w, r, http.StatusBadRequest, "error parsing form data", nil, err)
		return
	}
	data, err := f.delegate.LoadData(r)
	if err != nil {
		respond.Error(w, r, http.StatusNotFound, "not found", nil, err)
		return
	}
	fd := &FormPageData{
		FormID:    r.Form.Get("FormID"),
		FormToken: r.Form.Get("FormToken"),
		Data:      data,
	}
	dec := formam.NewDecoder(&formam.DecoderOptions{
		IgnoreUnknownKeys: true,
	})
	if err = dec.Decode(r.Form, fd.Data); err != nil {
		respond.Error(w, r, http.StatusUnprocessableEntity, "error unserializing form data", nil, err)
		return
	}
	if err = fd.validateFormToken(f.store); err != nil {
		respond.Error(w, r, http.StatusUnprocessableEntity, "form token error", nil, err)
		return
	}

	if err = f.store.Delete(fd.FormID); err != nil {
		respond.Error(w, r, http.StatusInternalServerError, "form token error", nil, err)
		return
	}

	if fd.Errors = f.maybeValidate(r, fd.Data); len(fd.Errors) == 0 {
		if !f.delegate.Submit(w, r, fd.Data).Do(w, r, fd) {
			return
		}
	}

	f.buildForm(w, r, session.Get(r), fd)
}

func (f *Form) buildForm(w http.ResponseWriter, r *http.Request, sess *session.Session, fd *FormPageData) {
	logger := server.GetLogger(r)
	if err := fd.regenerateFormToken(f.store); err != nil {
		logger.WithError(err).Errorln("failed to create form token")
	}
	respond.Page(logger, w, f.page, f.title, sess, f.delegate.GetAccessCheck(r), fd)
}

func (f *Form) maybeValidate(r *http.Request, v interface{}) []string {
	if vf, ok := f.delegate.(Validator); ok {
		return vf.Validate(r, v)
	}

	return nil
}

func (f *Form) Pages(path string, middlewares ...negroni.Handler) []server.Route {
	return []server.Route{
		{http.MethodGet, path, server.WrapF(f.Page, middlewares...)},
		{http.MethodPost, path, server.WrapF(f.Submit, middlewares...)},
	}
}

// Delegate is a helper type that contains the form presentation and logic.
type Delegate interface {
	GetAccessCheck(r *http.Request) page.AccessChecker
	LoadData(r *http.Request) (interface{}, error)
	Submit(w http.ResponseWriter, r *http.Request, v interface{}) FormSubmitResult
}

// Validator is a form delegate that validates the form data.
type Validator interface {
	Delegate
	Validate(r *http.Request, v interface{}) []string
}

// ErrInvalidFormContentType is an error that happens when a form submission
// happens with an incorrect content type.
type ErrInvalidFormContentType string

func (e ErrInvalidFormContentType) Error() string {
	return "invalid form content type: " + string(e)
}

func isMultipart(r *http.Request) bool {
	return r.Header.Get("Content-Type") == "multipart/form-data"
}

func isUrlEncoded(r *http.Request) bool {
	return r.Header.Get("Content-Type") == "application/x-www-form-urlencoded"
}

func parseForm(r *http.Request) error {
	if isMultipart(r) {
		if err := r.ParseMultipartForm(multipartFormBuffer); err != nil {
			return err
		}

		return nil
	} else if isUrlEncoded(r) {
		if err := r.ParseForm(); err != nil {
			return err
		}

		return nil
	}

	return ErrInvalidFormContentType(r.Header.Get("Content-Type"))
}

// FormPageData represents the form state.
//
// The Data attribute has the custom data that is either posted or loaded.
type FormPageData struct {
	Errors    []string
	FormID    string
	FormToken string
	Data      interface{}
}

func (f *FormPageData) generateFormID() {
	f.FormID = util.RandomHexString(formIDLength)
}

func (f *FormPageData) regenerateFormToken(storage keyvalue.Store) error {
	f.FormToken = util.RandomHexString(formTokenLength)
	return storage.SetExpiring(f.FormID, f.FormToken, 24*time.Hour)
}

func (f *FormPageData) validateFormToken(storage keyvalue.Store) error {
	res, err := storage.Get(f.FormID)
	if err != nil {
		return err
	}

	if res != f.FormToken {
		return errors.New("form token mismatch")
	}

	return nil
}

func (f *FormPageData) CSRFToken() template.HTML {
	return template.HTML(`
		<input type="hidden" name="FormID" value="` + f.FormID + `" />
		<input type="hidden" name="FormToken" value="` + f.FormToken + `" />
	`)
}

func (f *FormPageData) ErrorMessages() template.HTML {
	if len(f.Errors) == 0 {
		return ""
	}

	tpl := `<div class="messages error">`
	for _, err := range f.Errors {
		tpl += `<p class="error">` + html.EscapeString(err) + `</p>`
	}
	tpl += `</div>`

	return template.HTML(tpl)
}

// FormSubmitResult represents what should happen after a form submit.
type FormSubmitResult interface {
	// Do is the action, returning whether the form is needed to be rebuilt
	// or not.
	Do(w http.ResponseWriter, r *http.Request, fd *FormPageData) bool
}

type redirectResult struct {
	path string
}

func (res redirectResult) Do(w http.ResponseWriter, r *http.Request, _ *FormPageData) bool {
	http.Redirect(w, r, res.path, http.StatusFound)
	return false
}

// Redirect tells a form to redirect after submit.
func Redirect(path string) FormSubmitResult {
	if path == "" {
		path = "/"
	}
	return redirectResult{
		path: path,
	}
}

type errorResult struct {
	message string
	err     error
}

func (res errorResult) Do(_ http.ResponseWriter, r *http.Request, fd *FormPageData) bool {
	logger := server.GetLogger(r)
	logger.WithError(res.err).Warnln("failed to submit form")
	fd.Errors = append(fd.Errors, res.message)
	if err := database.MaybeRollback(r); err != nil {
		logger.WithError(err).Errorln("failed to roll back transaction")
	}

	return true
}

// Error tells a form that an error happened during the form submission.
func Error(message string, err error) FormSubmitResult {
	return errorResult{
		message: message,
		err:     err,
	}
}
