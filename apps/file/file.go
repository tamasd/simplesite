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

package file

import (
	"io/ioutil"
	"net/http"
	"path"

	"github.com/lpar/gzipped"
	"github.com/sirupsen/logrus"
	"github.com/tamasd/simplesite/server"
)

// AssetDir returns a route for the assets/ directory.
//
// If there is a compressed version of a file available, it will be served
// instead if the client supports it.
func AssetDir() server.Route {
	return server.Route{
		Method:  http.MethodGet,
		Path:    "/assets/*filepath",
		Handler: http.StripPrefix("/assets", gzipped.FileServer(http.Dir("./assets"))),
	}
}

// MiscDir returns routes for the misc/ directory.
//
// This is a special directory where each file will be a route under /. The
// point of this is create a simple solution for paths like favicon.ico or
// robots.txt.
func MiscDir(logger logrus.FieldLogger) []server.Route {
	var routes []server.Route

	files, err := ioutil.ReadDir("misc/")
	if err != nil {
		logger.WithError(err).Errorln("failed to list misc directory")
		return nil
	}

	for _, fn := range files {
		func(fn string) {
			fp := path.Join("misc", fn)
			logger.WithFields(logrus.Fields{
				"filename": fn,
				"filepath": fp,
			}).Infoln("generating route for file")
			routes = append(routes, server.Route{
				Method: http.MethodGet,
				Path:   "/" + fn,
				Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					http.ServeFile(w, r, fp)
				}),
			})
		}(fn.Name())
	}

	return routes
}
