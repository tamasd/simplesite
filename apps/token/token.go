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

package token

import (
	"net/http"
	"time"

	uuid "github.com/satori/go.uuid"
	"github.com/sirupsen/logrus"
	"github.com/tamasd/simplesite/database"
	"github.com/tamasd/simplesite/server"
	"github.com/tamasd/simplesite/util"
)

const (
	tokenLen = 64
)

// Token is a manager for the token entity.
type Token struct {
	conn   database.DB
	logger logrus.FieldLogger
}

// NewToken creates a new manager for the token entity.
func NewToken(logger logrus.FieldLogger, conn database.DB) *Token {
	return &Token{
		conn:   conn,
		logger: logger,
	}
}

// NewTokenFromRequest returns the token manager from the request context.
func NewTokenFromRequest(r *http.Request) *Token {
	return NewToken(server.GetLogger(r), database.Get(r))
}

// SchemaSQL returns the schema for tokens.
func (t Token) SchemaSQL() string {
	return `
		CREATE TABLE IF NOT EXISTS token (
			uuid uuid NOT NULL,
			category character varying NOT NULL,
			token character(128) NOT NULL,
			expires timestamp with time zone,
			CONSTRAINT token_pkey PRIMARY KEY (uuid, category),
			CONSTRAINT token_token_key UNIQUE (token)
		);
	`
}

// Create generates a token for a given uuid and category, with an optional
// expiration.
func (t *Token) Create(uuid uuid.UUID, category string, expires *time.Time) (string, error) {
	token := util.RandomHexString(tokenLen)
	return token, t.setToken(uuid, category, token, expires)
}

func (t *Token) setToken(uuid uuid.UUID, category, token string, expires *time.Time) error {
	if err := t.autoclean(uuid, category); err != nil {
		return err
	}

	_, err := t.conn.Exec(`INSERT INTO token(uuid, category, token, expires) VALUES($1, $2, $3, $4)`,
		uuid,
		category,
		token,
		expires,
	)
	return err
}

func (t *Token) autoclean(uuid uuid.UUID, category string) error {
	_, err := t.conn.Exec(`DELETE FROM token WHERE uuid = $1 AND category = $2`, uuid, category)
	return err
}

// Consume consumes an active (not expired) token that is linked to an uuid
// and a category.
func (t *Token) Consume(uuid uuid.UUID, category, token string) (bool, error) {
	res, err := t.conn.Exec(`DELETE FROM token WHERE uuid = $1 AND category = $2 AND token = $3 AND (expires IS NULL OR expires > $4)`,
		uuid,
		category,
		token,
		time.Now(),
	)

	if err != nil {
		return false, err
	}

	aff, err := res.RowsAffected()

	return aff > 0, err
}

// RemoveExpired removes expired tokens from the database.
func (t *Token) RemoveExpired() error {
	_, err := t.conn.Exec(`DELETE FROM token WHERE expires < $1`, time.Now())
	return err
}
