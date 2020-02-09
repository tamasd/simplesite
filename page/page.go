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

package page

import (
	"html/template"
	"net/http"

	"github.com/sirupsen/logrus"
	"github.com/tamasd/simplesite/server"
	"github.com/tamasd/simplesite/util"
	"github.com/urfave/negroni"
)

const (
	entityLoaderContextKey = "entity-loader"
)

var (
	// BasePage is the main page template.
	BasePage = template.Must(template.New("BasePage").Parse(`<!DOCTYPE HTML>
<html>
<head>
	<meta http-equiv="X-UA-Compatible" content="IE=edge,chrome=1" />
	<meta charset="utf8" />
	<link rel="stylesheet" href="/assets/style.css" />
	<link rel="author" href="/humans.txt" />
	<title>{{.Title}}</title>
    <script type="text/javascript" nonce="{{.Nonce}}">
        window.CSRF_TOKEN = "{{.CSRFToken}}";
    </script>
</head>
<body>
	<header>
		<nav>
			<ul>
				<li class="home"><a href="/">Home</a></li>
				<li class="posts"><a href="/posts">Posts</a></li>
				{{if .LoggedIn}}
				<li class="logout"><a href="/logout?token={{.CSRFToken}}">Logout</a></li>
				{{else}}
				<li class="login"><a href="/login">Log In</a></li>
				<li class="register"><a href="/register">Register</a></li>
				{{end}}
			</ul>
		</nav>
	</header>
	<div id="body">{{block "body" .Body}}{{end}}</div>
</body>
</html>
{{define "secondary-menu"}}	
<nav>
	<ul>
		{{block "secondary-menu-items" .}}{{end}}
	</ul>
</nav>
{{end}}
`))
)

// AccessChecker checks if the current account has a permission.
type AccessChecker interface {
	Has(name string) bool
}

// Data is the page data for BasePage.
type Data struct {
	Title     string
	Nonce     string
	CSRFToken string
	LoggedIn  bool
	Access    AccessChecker
	Body      interface{}
}

func (d Data) Has(name string) bool {
	if d.Access != nil {
		return d.Access.Has(name)
	}

	return false
}

// SubPage creates a template that uses the base page.
func SubPage(text string, extra ...string) *template.Template {
	tpl := template.Must(BasePage.Clone())

	for _, t := range extra {
		tpl = template.Must(tpl.Parse(t))
	}

	return template.Must(tpl.Parse(text))
}

// GetEntity returns the current entity that is referenced in the URL.
func GetEntity(r *http.Request) (interface{}, error) {
	container := r.Context().Value(entityLoaderContextKey).(*entityContainer)
	container.load(r)
	return container.entity, container.err
}

type entityContainer struct {
	logger logrus.FieldLogger
	loader EntityLoader

	loaded bool
	entity interface{}
	err    error
}

func (c *entityContainer) load(r *http.Request) {
	if c.loaded {
		return
	}
	c.loaded = true

	c.entity, c.err = c.loader.Load(r)
}

// EntityLoader loads an entity.
type EntityLoader interface {
	Load(r *http.Request) (interface{}, error)
}

// EntityLoaderFunc is a single function implementation of EntityLoader.
type EntityLoaderFunc func(r *http.Request) (interface{}, error)

func (f EntityLoaderFunc) Load(r *http.Request) (interface{}, error) {
	return f(r)
}

type entityLoader struct {
	loader EntityLoader
}

func (e *entityLoader) ServeHTTP(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	next(w, util.SetContext(r, entityLoaderContextKey, &entityContainer{
		logger: server.GetLogger(r),
		loader: e.loader,
	}))
}

// EntityLoaderMiddleware creates a middleware that loads an entity from the
// URL.
func EntityLoaderMiddleware(loader EntityLoader) negroni.Handler {
	return &entityLoader{
		loader: loader,
	}
}
