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

package post

import (
	"bytes"
	"html"
	"html/template"
	"net/http"
	"path"
	"strings"

	"github.com/julienschmidt/httprouter"
	uuid "github.com/satori/go.uuid"
	"github.com/sergi/go-diff/diffmatchpatch"
	"github.com/tamasd/simplesite/apps/account"
	"github.com/tamasd/simplesite/database"
	"github.com/tamasd/simplesite/form"
	"github.com/tamasd/simplesite/keyvalue"
	"github.com/tamasd/simplesite/page"
	"github.com/tamasd/simplesite/respond"
	"github.com/tamasd/simplesite/server"
	"github.com/tamasd/simplesite/session"
	"github.com/tamasd/simplesite/util"
	"github.com/urfave/negroni"
)

const (
	// PermissionCreatePost is the permission for creating posts.
	PermissionCreatePost = "create-post"
	// PermissionEditOwnPost is the permission for editing own posts.
	PermissionEditOwnPost = "edit-own-post"
	// PermissionEditAnyPost is the permission for editing any posts.
	PermissionEditAnyPost = "edit-any-post"

	// PageSize is the default page size for post listing pages.
	PageSize = 15

	postContextKey = "post"
)

var (
	diff = diffmatchpatch.New()

	postWidget = `
{{define "post"}}
	<article class="post">
		<header><h2>{{.Post.Title}}</h2></header>
		<section class="post">
			{{.Revision.Filtered}}
		</section>
		<footer>
		{{if .CanEdit}}
			<a class="edit" href="/post/{{.Post.ID}}/edit">Edit</a>	|
			<a class="revisions" href="/post/{{.Post.ID}}/revisions">Revisions</a>
		{{end}}
		</footer>
	</article>
{{end}}
`

	listingPage = page.SubPage(`
{{define "secondary-menu-items"}}
	{{if .CanCreate}}
		<a href="/posts/create">Create post</a>
	{{end}}
{{end}}
{{define "body"}}
	{{template "secondary-menu" .}}
	{{range .Posts}}
		{{template "post" .}}
	{{else}}
	No posts found
	{{end}}
{{end}}
`, postWidget)

	postFormPage = page.SubPage(`
{{define "body"}}
<form method="POST">
	{{.ErrorMessages}}
	{{.CSRFToken}}
	<p><label>Title: <br/><input type="textfield" name="Title" value="{{.Data.Title}}" /></label></p>
	<p><label>Content: <br/><textarea name="Content">{{.Data.Content}}</textarea></label></p>
	<p><input type="submit" value="Save" /></p>
</form>
{{end}}
`)

	revisionsFormPage = page.SubPage(`
{{define "body"}}
<form method="POST">
	{{.ErrorMessages}}
	{{.CSRFToken}}
	<table>
		<thead>
			<th colspan="4">Created</th>
		</thead>
		<tbody>
			{{range .Data.Revisions}}
			<tr>
				<td class="maxwidth">{{.Revision.Created}}</td>
				<td class="diff-set">
					{{if .Active}}
					Current
					{{else}}
					<button type="submit" name="Op" value="set:{{.Revision.ID}}">Set</button>
					{{end}}
				</td>
				<td class="diff-radio"><input type="radio" name="Diff0" value="{{.Revision.ID}}" /></td>
				<td class="diff-radio"><input type="radio" name="Diff1" value="{{.Revision.ID}}" /></td>
			</tr>
			{{end}}
		</tbody>
	</table>
	<div class="align-right">
		<button type="submit" name="Op" value="diff">Diff</button>
	</div>
</form>
{{end}}
`)

	postDiffPage = page.SubPage(`
{{define "body"}}
	<div class="diff">
	{{.Diff}}
	</div>
{{end}}
`)
)

type postWidgetData struct {
	*PostRecord
	CanEdit bool
}

type listingPageData struct {
	Posts     []postWidgetData
	CanCreate bool
}

type postFormPageData struct {
	Title   string
	Content string
}

type revisionsFormPageData struct {
	Revisions []revisionsFormRecordData
	Op        string
	Diff0     string
	Diff1     string
}

type revisionsFormRecordData struct {
	Revision *PostRevision
	Active   bool
}

type postDiffPageData struct {
	Diff template.HTML
}

// Pages returns the list of routes for the post entity.
func Pages(store keyvalue.Store, filter func(string) string) []server.Route {
	txmw := database.NewTxMiddleware(true)
	el := page.EntityLoaderMiddleware(page.EntityLoaderFunc(LoadEntity))
	pmw := EnsurePostMiddleware()
	eamw := PostEditAccessMiddleware()

	routes := []server.Route{
		{http.MethodGet, "/posts", ListPage()},
		{http.MethodGet, "/post/:id/revisions/:r0/:r1", server.Wrap(RevisionDiffPage(), el, pmw, eamw)},
	}

	routes = append(routes, form.NewForm(store, "Create post", postFormPage, NewPostForm(filter)).
		Pages("/posts/create", account.EnforcePermission(PermissionCreatePost), txmw, el)...)
	routes = append(routes, form.NewForm(store, "Edit post", postFormPage, NewPostForm(filter)).
		Pages("/post/:id/edit", txmw, el, pmw, eamw)...)
	routes = append(routes, form.NewForm(store, "Revisions", revisionsFormPage, NewRevisionsForm()).
		Pages("/post/:id/revisions", txmw, el, pmw, eamw)...)

	return routes
}

// ListPage is a http handler that lists posts.
func ListPage() http.Handler {
	return server.WrapF(func(w http.ResponseWriter, r *http.Request) {
		logger := server.GetLogger(r)
		sess := session.Get(r)
		conn := database.Get(r)
		access := account.GetAccessChecker(r)

		data := listingPageData{
			CanCreate: access.Has(PermissionCreatePost),
		}

		records, err := listPostsByCondition(conn, PageSize, 0, "")
		if err != nil {
			respond.Error(w, r, http.StatusInternalServerError, "error listing posts", nil, err)
			return
		}

		for _, record := range records {
			data.Posts = append(data.Posts, postWidgetData{
				PostRecord: record,
				CanEdit:    canEdit(sess.ID, record.Revision.Author, access),
			})
		}

		respond.Page(logger, w, listingPage, "Posts", sess, access, data)
	})
}

// RevisionDiffPage is a http handler that shows a diff page between two
// revisions of a post.
func RevisionDiffPage() http.Handler {
	return server.WrapF(func(w http.ResponseWriter, r *http.Request) {
		logger := server.GetLogger(r)
		sess := session.Get(r)
		access := account.GetAccessChecker(r)
		conn := database.Get(r)
		post := GetPostRecord(r)
		params := httprouter.ParamsFromContext(r.Context())

		revs, err := mustLoadRevisionsFromStrings(conn, post.Post.ID, params.ByName("r0"), params.ByName("r1"))
		if err != nil {
			respond.Error(w, r, http.StatusNotFound, "not found", nil, err)
			return
		}

		diffs := diff.DiffMain(revs[1].Content, revs[0].Content, true)

		respond.Page(logger, w, postDiffPage, "Diff", sess, access, postDiffPageData{
			Diff: renderDiffs(diffs),
		})
	})
}

func renderDiffs(diffs []diffmatchpatch.Diff) template.HTML {
	buf := bytes.NewBuffer(nil)

	for _, diff := range diffs {
		text := strings.Replace(html.EscapeString(diff.Text), "\n", "&para;<br />", -1)
		switch diff.Type {
		case diffmatchpatch.DiffInsert:
			_, _ = buf.WriteString("<ins>" + text + "</ins>")
		case diffmatchpatch.DiffDelete:
			_, _ = buf.WriteString("<del>" + text + "</del>")
		case diffmatchpatch.DiffEqual:
			_, _ = buf.WriteString("<span>" + text + "</span>")
		}
	}

	return template.HTML(buf.Bytes())
}

func canEdit(uid uuid.UUID, author uuid.UUID, access page.AccessChecker) bool {
	if !uuid.Equal(uid, uuid.Nil) {
		if access.Has(PermissionEditAnyPost) {
			return true
		} else if uuid.Equal(uid, author) {
			return access.Has(PermissionEditOwnPost)
		}
	}

	return false
}

type postForm struct {
	account.AccessCheckLoader
	filter func(string) string
}

func (p *postForm) LoadData(r *http.Request) (interface{}, error) {
	entity, err := page.GetEntity(r)
	if err != nil {
		return nil, err
	}

	if entity == nil {
		return &postFormPageData{}, nil
	}

	data := entity.(*PostRecord)

	return &postFormPageData{
		Title:   data.Post.Title,
		Content: data.Revision.Content,
	}, nil
}

func (p *postForm) Validate(_ *http.Request, v interface{}) []string {
	var errs []string
	rec := v.(*postFormPageData)

	if strings.TrimSpace(rec.Title) == "" {
		errs = append(errs, "Title is required")
	}

	return errs
}

func (p *postForm) Submit(_ http.ResponseWriter, r *http.Request, v interface{}) form.FormSubmitResult {
	conn := database.Get(r)
	rec := v.(*postFormPageData)
	sess := session.Get(r)

	entity, err := page.GetEntity(r)
	if err != nil {
		return form.Error("Failed to load entity", err)
	}

	if entity == nil {
		entity = &PostRecord{
			Post:     &Post{},
			Revision: &PostRevision{},
		}
	}

	data := entity.(*PostRecord)
	data.Post.Title = rec.Title
	data.Revision.Content = rec.Content
	data.Revision.Filtered = template.HTML(p.filter(rec.Content))
	data.Revision.Author = sess.ID

	if err = data.Save(conn); err != nil {
		return form.Error("Cannot save post", err)
	}

	return form.Redirect("/posts")
}

// NewPostForm creates the delegate for the post form.
//
// This form handles the creating and editing of a post.
func NewPostForm(filter func(string) string) form.Delegate {
	return &postForm{
		filter: filter,
	}
}

type revisionsForm struct {
	account.AccessCheckLoader
}

// NewRevisionsForm creates the delegate for the post revision form page.
func NewRevisionsForm() form.Delegate {
	return &revisionsForm{}
}

func (f *revisionsForm) LoadData(r *http.Request) (interface{}, error) {
	conn := database.Get(r)
	record := GetPostRecord(r)

	revs, err := ListRevisions(conn, record.Post.ID)
	if err != nil {
		return nil, err
	}

	records := make([]revisionsFormRecordData, len(revs))
	for i, rev := range revs {
		records[i] = revisionsFormRecordData{
			Revision: rev,
			Active:   uuid.Equal(record.Post.Revision, rev.ID),
		}
	}

	return &revisionsFormPageData{
		Revisions: records,
	}, err
}

func (f *revisionsForm) Validate(_ *http.Request, v interface{}) []string {
	var errs []string
	data := v.(*revisionsFormPageData)
	if data.Op == "diff" {
		if data.Diff0 == data.Diff1 {
			errs = append(errs, "Cannot diff the same revision")
		}
	} else if !strings.HasPrefix(data.Op, "set:") {
		errs = append(errs, "Invalid form operation")
	}

	return errs
}

func (f *revisionsForm) Submit(_ http.ResponseWriter, r *http.Request, v interface{}) form.FormSubmitResult {
	data := v.(*revisionsFormPageData)
	rec := GetPostRecord(r)
	conn := database.Get(r)

	if data.Op == "diff" {
		redir := *r.URL
		redir.Path = path.Join(redir.Path, data.Diff0, data.Diff1)
		return form.Redirect(redir.String())
	}

	newrev, err := uuid.FromString(data.Op[4:])
	if err != nil {
		return form.Error("Invalid form operation", err)
	}

	rec.Post.Publish(newrev)

	if err = rec.Post.Save(conn); err != nil {
		return form.Error("Cannot publish revision", err)
	}

	return form.Redirect("/posts")
}

type postEditAccessMiddleware struct{}

func (p *postEditAccessMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	access := account.GetAccessChecker(r)

	sess := session.Get(r)
	record := GetPostRecord(r)

	if !canEdit(sess.ID, record.Revision.Author, access) {
		account.RespondPermissionDenied(w, r, "edit-post")
		return
	}

	next(w, r)
}

// PostEditAccessMiddleware is a middleware that makes sure that the current
// account has edit access on the post in the URL.
func PostEditAccessMiddleware() negroni.Handler {
	return &postEditAccessMiddleware{}
}

type ensurePostMiddleware struct{}

// EnsurePostMiddleware loads the post record object from the URL.
func EnsurePostMiddleware() negroni.Handler {
	return &ensurePostMiddleware{}
}

func (m *ensurePostMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	entity, err := page.GetEntity(r)
	if err != nil {
		respond.Error(w, r, http.StatusInternalServerError, "failed to load entity", nil, err)
		return
	}

	if entity == nil {
		respond.Error(w, r, http.StatusNotFound, "entity not found", nil, err)
		return
	}

	next(w, util.SetContext(r, postContextKey, entity.(*PostRecord)))
}

// GetPostRecord returns the loaded PostRecord from the request context.
func GetPostRecord(r *http.Request) *PostRecord {
	return r.Context().Value(postContextKey).(*PostRecord)
}
