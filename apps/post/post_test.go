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

package post_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	lorem "github.com/drhodes/golorem"
	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/require"
	"github.com/tamasd/simplesite/apps/account"
	"github.com/tamasd/simplesite/apps/post"
	"github.com/tamasd/simplesite/util/testutil"
)

func TestPostCRUD(t *testing.T) {
	srv := testutil.SetupTestSiteFromEnv()
	defer srv.Cleanup()

	conn := srv.Database()
	admin := srv.CreateClient(t)
	anon := srv.CreateClient(t)

	regdata := testutil.TestRegData()
	admin.RegistrationAndLogin(regdata)

	uid := admin.CurrentUID()
	require.False(t, uuid.Equal(uid, uuid.Nil))

	err := account.SavePermissions(conn, uid, account.Permissions{
		post.PermissionCreatePost,
		post.PermissionEditOwnPost,
		post.PermissionEditAnyPost,
	})
	require.Nil(t, err)

	createPostData := &url.Values{}
	createPostData.Set("Title", lorem.Sentence(1, 8))
	createPostData.Set("Content", lorem.Paragraph(8, 16))
	adminresp := admin.Form("/posts/create").Submit(createPostData)
	require.Equal(t, http.StatusFound, adminresp.StatusCode)
	admin.FollowRedirect()

	require.Equal(t, createPostData.Get("Title"), admin.Page.Find("article.post header h2").First().Text())
	require.Equal(t, createPostData.Get("Content"), strings.TrimSpace(admin.Page.Find("article.post section.post").First().Text()))
	require.NotEqual(t, 0, admin.Page.Find(`a[href="/posts/create"]`).Length())
	require.NotEqual(t, 0, admin.Page.Find(`footer a.edit`).Length())
	require.NotEqual(t, 0, admin.Page.Find(`footer a.revisions`).Length())

	anonresp := anon.Request(http.MethodGet, "/posts", nil)
	require.Equal(t, http.StatusOK, anonresp.StatusCode)
	require.Equal(t, 0, anon.Page.Find(`a[href="/posts/create"]`).Length())
	require.Equal(t, 0, anon.Page.Find(`footer a.edit`).Length())
	require.Equal(t, 0, anon.Page.Find(`footer a.revisions`).Length())

	href := admin.Page.Find("article.post footer a.edit").AttrOr("href", "")
	require.NotZero(t, href)
	sf := admin.Form(href)
	editPostData := admin.FormValues("")
	require.Equal(t, createPostData.Get("Title"), editPostData.Get("Title"))
	require.Equal(t, createPostData.Get("Content"), editPostData.Get("Content"))
	editPostData.Set("Content", lorem.Paragraph(8, 16))
	resp := sf.Submit(editPostData)
	require.Equal(t, http.StatusFound, resp.StatusCode)
	admin.FollowRedirect()

	require.Equal(t, editPostData.Get("Title"), admin.Page.Find("article.post header h2").First().Text())
	require.Equal(t, editPostData.Get("Content"), strings.TrimSpace(admin.Page.Find("article.post section.post").First().Text()))

	href = admin.Page.Find("article.post footer a.revisions").AttrOr("href", "")
	require.NotZero(t, href)
	sf = admin.Form(href)
	revisionFormData := &url.Values{}
	setRevisionButton := admin.Page.Find("td.diff-set button[type=submit][name=Op]").AttrOr("value", "")
	require.NotZero(t, setRevisionButton)
	require.True(t, strings.HasPrefix(setRevisionButton, "set:"))
	revisionFormData.Set("Op", setRevisionButton)
	adminresp = sf.Submit(revisionFormData)
	require.Equal(t, http.StatusFound, adminresp.StatusCode)
	admin.FollowRedirect()

	require.Equal(t, createPostData.Get("Title"), admin.Page.Find("article.post header h2").First().Text())
	require.Equal(t, createPostData.Get("Content"), strings.TrimSpace(admin.Page.Find("article.post section.post").First().Text()))
}
