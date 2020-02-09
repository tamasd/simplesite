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
	"net/http"
	"strconv"

	uuid "github.com/satori/go.uuid"
	"github.com/sirupsen/logrus"
	"github.com/tamasd/simplesite/database"
	"github.com/tamasd/simplesite/page"
	"github.com/tamasd/simplesite/respond"
	"github.com/tamasd/simplesite/server"
	"github.com/tamasd/simplesite/session"
	"github.com/tamasd/simplesite/util"
	"github.com/urfave/negroni"
)

const (
	permContextKey = "permissions"
)

// Permissions represent the set of permissions that an account has.
type Permissions []string

// Has checks if an account has a permission or not.
//
// While this is using a linear search, it should be fine since an account
// won't have more than a few dozen permissions.
func (p Permissions) Has(perm string) bool {
	for _, i := range p {
		if i == perm {
			return true
		}
	}

	return false
}

// AccessCheckLoader adds the default access check loader to a form.
//
// This type is meant to be embedded in a form delegate.
type AccessCheckLoader struct{}

// GetAccessCheck returns the access checker saved in the request context.
//
// This function helps a form delegate to implement form.Delegate by using
// the GetAccessChecker().
func (l AccessCheckLoader) GetAccessCheck(r *http.Request) page.AccessChecker {
	return GetAccessChecker(r)
}

// GetAccessChecker returns the access checker saved in the request context.
func GetAccessChecker(r *http.Request) page.AccessChecker {
	return r.Context().Value(permContextKey).(page.AccessChecker)
}

type accessChecker struct {
	permissions Permissions
	loaded      bool
	r           *http.Request
}

func (ac *accessChecker) load() {
	defer func() {
		ac.loaded = true
	}()

	uid := session.Get(ac.r).ID
	if uuid.Equal(uid, uuid.Nil) {
		return
	}

	conn := database.Get(ac.r)
	logger := server.GetLogger(ac.r)

	var err error
	ac.permissions, err = LoadPermissions(conn, uid)
	if err != nil {
		logger.WithError(err).WithFields(logrus.Fields{
			"uid": uid.String(),
		}).Errorln("failed to load permissions")
	}
}

// Has implements page.AccessChecker.Has().
func (ac *accessChecker) Has(name string) bool {
	if !ac.loaded {
		ac.load()
	}

	if ac.permissions == nil {
		return false
	}

	return ac.permissions.Has(name)
}

// Permission represents data from the permission table.
type Permission struct {
	ID         uuid.UUID `json:"id"`
	Permission string    `json:"permission"`
}

// SchemaSQL returns the database schema for the permission table.
func (p Permission) SchemaSQL() string {
	return `
		CREATE TABLE permission (
			id uuid NOT NULL
				CONSTRAINT permission_account_id_fk
				REFERENCES account ON UPDATE CASCADE ON DELETE CASCADE,
			permission character varying NOT NULL,
			CONSTRAINT permission_pk PRIMARY KEY (id, permission)
		);
	`
}

// LoadPermissions loads the list of permissions for a given account.
func LoadPermissions(conn database.DB, id uuid.UUID) (Permissions, error) {
	var perms []string

	rows, err := conn.Query(`SELECT permission FROM permission WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}

	for rows.Next() {
		var p string
		if err = rows.Scan(&p); err != nil {
			return nil, err
		}
		perms = append(perms, p)
	}

	return perms, nil
}

// SavePermissions overwrites the permissions for a given account.
//
// It is strongly recommended that the database connection given to this
// function is a transaction.
func SavePermissions(conn database.DB, id uuid.UUID, p Permissions) error {
	_, err := conn.Exec(`DELETE FROM permission WHERE id = $1`, id)
	if err != nil {
		return err
	}

	if len(p) == 0 {
		return nil
	}

	query := `INSERT INTO permission(id, permission) VALUES `
	args := make([]interface{}, 1+len(p))
	args[0] = id
	for i, perm := range p {
		args[i+1] = perm
		query += `($1, $` + strconv.Itoa(i+2) + `), `
	}

	_, err = conn.Exec(query[:len(query)-2], args...)
	return err
}

type permissionLoaderMiddleware struct{}

// PreloadPermissions is a middleware that lazy-loads permissions for a given
// account.
func PreloadPermissions() negroni.Handler {
	return &permissionLoaderMiddleware{}
}

func (m *permissionLoaderMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	next(w, util.SetContext(r, permContextKey, &accessChecker{r: r}))
}

type permissionEnforcerMiddleware struct {
	name string
}

// EnforcePermission is a middleware that makes sure the current account has the
// given permission before proceeding on the middleware chain.
func EnforcePermission(perm string) negroni.Handler {
	return &permissionEnforcerMiddleware{
		name: perm,
	}
}

func (m *permissionEnforcerMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	if !GetAccessChecker(r).Has(m.name) {
		RespondPermissionDenied(w, r, m.name)
		return
	}
	next(w, r)
}

// RespondPermissionDenied responds with a permission denied page.
func RespondPermissionDenied(w http.ResponseWriter, r *http.Request, permName string) {
	respond.Error(w, r, http.StatusForbidden, "permission denied", logrus.Fields{
		"uid":        session.Get(r).ID.String(),
		"permission": permName,
	}, nil)
}
