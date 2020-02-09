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

package frontpage

import (
	"net/http"

	"github.com/tamasd/simplesite/apps/account"
	"github.com/tamasd/simplesite/page"
	"github.com/tamasd/simplesite/respond"
	"github.com/tamasd/simplesite/server"
	"github.com/tamasd/simplesite/session"
)

var (
	frontPage = page.SubPage(`
{{define "body"}}
<p>Lorem ipsum dolor sit amet, consectetur adipiscing elit. Nulla facilisis lacinia tortor, a pulvinar tellus consectetur at. In id quam sit amet neque condimentum congue et sagittis ante. Donec ut odio leo. Suspendisse massa quam, facilisis eu ultricies et, semper eu est. Curabitur auctor luctus sem, eu eleifend purus porta ultricies. Suspendisse egestas sollicitudin tortor semper molestie. Orci varius natoque penatibus et magnis dis parturient montes, nascetur ridiculus mus.</p>
<p>Nunc feugiat nulla ut sapien tristique rutrum non non sapien. Nullam nec convallis ligula. Etiam non dui pulvinar, eleifend nulla a, volutpat lectus. Integer non cursus orci. Aenean iaculis ex non sapien fringilla interdum. Ut euismod et est id suscipit. Aenean lacinia bibendum sem iaculis congue. Duis sed turpis viverra, ornare ligula in, aliquet nibh. Cras sapien erat, semper placerat elementum quis, cursus nec lectus. Ut viverra, tortor quis maximus malesuada, arcu odio maximus erat, in malesuada mauris tellus quis eros.</p>
<p>Donec non feugiat tortor. Orci varius natoque penatibus et magnis dis parturient montes, nascetur ridiculus mus. Mauris laoreet sed dolor vitae gravida. Vivamus quis sapien sed neque mollis iaculis. Nulla eu dolor vel neque facilisis rutrum a feugiat erat. Mauris vel volutpat nisl. In porta magna et purus consectetur maximus. Praesent mattis eleifend metus a rhoncus. Quisque mattis lacinia purus, sit amet ultrices ligula lacinia in. Duis feugiat vulputate sapien vitae aliquet. Quisque risus lacus, maximus a sagittis elementum, mollis molestie arcu. Nullam in metus et urna blandit imperdiet vel vel augue. Morbi ut cursus lacus. Fusce sit amet turpis purus. Proin sit amet ex nisi. Proin lacinia ipsum aliquet, laoreet nunc non, cursus lorem.</p>
<p>Etiam augue nisl, volutpat non rhoncus sed, auctor vel risus. Vestibulum quis mi a libero fermentum pellentesque. Mauris aliquet mattis nulla vitae faucibus. Sed dignissim enim sit amet massa semper, eu tempor felis finibus. Maecenas suscipit vestibulum elit, vitae accumsan est semper eu. Integer feugiat eros ac velit posuere, sed sollicitudin quam tincidunt. Donec dui ipsum, pharetra ut lacus in, vestibulum tempus mi. Sed vehicula nec dolor at finibus. Vestibulum eget dolor hendrerit, dictum elit eu, laoreet dui. Donec diam erat, ornare in sagittis a, cursus sit amet nulla. Ut ut tristique lacus. Maecenas gravida erat sed orci convallis consequat. Nunc magna mi, maximus eu diam ut, accumsan cursus dolor. Donec eget purus sed ante iaculis rhoncus.</p>
<p>In condimentum, leo id tempor condimentum, dolor urna ultricies est, eget vulputate justo ex non ligula. Sed aliquam ultricies condimentum. Sed iaculis mauris quis diam posuere rutrum. Vivamus quis tempor justo. Integer ac pellentesque risus, vel egestas nibh. Duis tempus et urna et facilisis. Proin volutpat in dolor eget ornare. Suspendisse ante dolor, malesuada ac cursus ut, tincidunt interdum mi. Nulla facilisi. In nulla diam, vestibulum lobortis sem quis, ornare tincidunt mi. Integer in efficitur lacus. Nunc vitae quam neque. Pellentesque habitant morbi tristique senectus et netus et malesuada fames ac turpis egestas. Nulla a ex non ante blandit varius. Phasellus finibus rutrum felis vitae suscipit. </p>
{{end}}
`)
)

// Page returns the route for the front page.
func Page() server.Route {
	return server.Route{
		Method:  http.MethodGet,
		Path:    "/",
		Handler: server.WrapF(Handler),
	}
}

type frontPageData struct{}

// Handler is the http handler for the front page.
func Handler(w http.ResponseWriter, r *http.Request) {
	sess := session.Get(r)
	logger := server.GetLogger(r)
	respond.Page(logger, w, frontPage, "Welcome", sess, account.GetAccessChecker(r), frontPageData{})
}
