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
	"database/sql"
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	"github.com/tamasd/simplesite/database"
	"github.com/tamasd/simplesite/util"
)

// PostRecord represents a post and its current revision.
type PostRecord struct {
	Post     *Post
	Revision *PostRevision
}

// Save inserts or updates a post and its active revision.
func (pr *PostRecord) Save(conn database.DB) error {
	if uuid.Equal(pr.Post.ID, uuid.Nil) {
		if err := pr.Post.Save(conn); err != nil {
			return err
		}
	}

	pr.Revision.Post = pr.Post.ID
	if err := pr.Revision.Save(conn); err != nil {
		return err
	}

	pr.Post.Publish(pr.Revision.ID)
	if err := pr.Post.Save(conn); err != nil {
		return err
	}

	return nil
}

// Post represents the post entity.
type Post struct {
	ID       uuid.UUID `json:"id"`
	Title    string    `json:"title"`
	Revision uuid.UUID `json:"revision"`
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
}

// SchemaSQL returns the schema for the post entity.
func (p Post) SchemaSQL() string {
	return `
		CREATE TABLE post (
			id uuid NOT NULL,
			revision uuid,
			title character varying NOT NULL,
			created timestamp with time zone NOT NULL DEFAULT now(),
			updated timestamp with time zone NOT NULL,
			PRIMARY KEY (id)
		);
	
		CREATE UNIQUE INDEX post_revision_unique ON post (revision)
			WHERE revision IS NOT NULL;
	`
}

// Publish sets a revision as the active one.
func (p *Post) Publish(revision uuid.UUID) {
	p.Revision = revision
}

// Unpublish removes the reference to the active revision.
//
// This hides the post from all of the public listing pages.
func (p *Post) Unpublish() {
	p.Revision = uuid.Nil
}

// Save inserts or updates a post.
func (p *Post) Save(conn database.DB) error {
	if uuid.Equal(p.ID, uuid.Nil) {
		p.ID = uuid.NewV4()
	}

	var revision interface{}
	if !uuid.Equal(p.Revision, uuid.Nil) {
		revision = p.Revision
	}

	_, err := conn.Exec(`
		INSERT INTO post (id, title, revision, updated)
		VALUES($1, $2, $3, $4)
		ON CONFLICT (id)
		DO UPDATE SET 
			title = $2,
			revision = $3,
			updated = $4
	`, p.ID, p.Title, revision, time.Now())

	return errors.Wrap(err, "error saving post")
}

// PostRevision represents a post's revision.
type PostRevision struct {
	ID       uuid.UUID     `json:"id"`
	Post     uuid.UUID     `json:"post"`
	Content  string        `json:"content"`
	Filtered template.HTML `json:"filtered"`
	Author   uuid.UUID     `json:"author"`
	Created  time.Time     `json:"created"`
}

// SchemaSQL returns the schema of the PostRevision.
func (r PostRevision) SchemaSQL() string {
	return `
		CREATE TABLE post_revision (
			id uuid NOT NULL,
			post uuid NOT NULL
				REFERENCES post(id) ON UPDATE CASCADE ON DELETE CASCADE,
			content text NOT NULL,
			filtered text NOT NULL,
			author uuid NOT NULL
				REFERENCES account(id) ON UPDATE CASCADE ON DELETE CASCADE,
			created timestamp with time zone NOT NULL DEFAULT now(),
			PRIMARY KEY (id)
		);
	
		ALTER TABLE post ADD 
			CONSTRAINT post_revision_fk FOREIGN KEY (revision)
			REFERENCES post_revision(id) ON UPDATE CASCADE ON DELETE CASCADE;
	`
}

// Save inserts a new revision.
func (r *PostRevision) Save(conn database.DB) error {
	r.ID = uuid.NewV4()

	_, err := conn.Exec(`
		INSERT INTO post_revision (id, post, content, filtered, author)
		VALUES($1, $2, $3, $4, $5)
	`,
		r.ID,
		r.Post,
		r.Content,
		r.Filtered,
		r.Author,
	)

	return errors.Wrap(err, "error saving post revision")
}

func listPostsByCondition(conn database.DB, limit, offset int, condition string, args ...interface{}) ([]*PostRecord, error) {
	var records []*PostRecord
	if condition != "" {
		condition = `WHERE ` + condition
	}
	rows, err := conn.Query(fmt.Sprintf(`
		SELECT 
			p.id, p.title, p.created, p.updated,
			r.id, r.content, r.filtered, r.author, r.created
		FROM post p JOIN post_revision r ON p.revision = r.id
		`+condition+`
		ORDER BY p.updated DESC
		LIMIT %d OFFSET %d
	`, limit, offset), args...)
	if err != nil {
		return nil, err
	}

	for rows.Next() {
		post := &Post{}
		revision := &PostRevision{}
		if err = rows.Scan(
			&post.ID,
			&post.Title,
			&post.Created,
			&post.Updated,
			&revision.ID,
			&revision.Content,
			&revision.Filtered,
			&revision.Author,
			&revision.Created,
		); err != nil {
			return nil, err
		}

		post.Revision = revision.ID
		revision.Post = post.ID

		records = append(records, &PostRecord{
			Post:     post,
			Revision: revision,
		})
	}

	return records, nil
}

func listRevisionsByCondition(conn database.DB, condition string, args ...interface{}) ([]*PostRevision, error) {
	var revs []*PostRevision

	if condition != "" {
		condition = `WHERE ` + condition
	}
	rows, err := conn.Query(`
		SELECT id, post, content, filtered, author, created
		FROM post_revision
		`+condition+`
		ORDER BY created DESC
	`, args...)
	if err != nil {
		return nil, err
	}

	for rows.Next() {
		r := &PostRevision{}
		if err = rows.Scan(
			&r.ID,
			&r.Post,
			&r.Content,
			&r.Filtered,
			&r.Author,
			&r.Created,
		); err != nil {
			return nil, err
		}

		revs = append(revs, r)
	}

	return revs, nil
}

// ListRevisions lists revisions for a given post.
func ListRevisions(conn database.DB, pid uuid.UUID) ([]*PostRevision, error) {
	return listRevisionsByCondition(conn, "post = $1", pid)
}

// LoadEntityFromUrl loads a post from the URL.
//
// The 'param' tells the name of the parameter where the post's uuid is.
func LoadEntityFromUrl(r *http.Request, param string) (interface{}, error) {
	conn := database.Get(r)

	idstr := httprouter.ParamsFromContext(r.Context()).ByName(param)
	if idstr == "" {
		return nil, nil
	}

	id, err := uuid.FromString(idstr)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse uuid in url")
	}

	recs, err := listPostsByCondition(conn, 1, 0, "p.id = $1", id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to load post")
	}

	return recs[0], nil
}

// LoadEntity loads an entity from the URL with the default parameter name 'id'.
func LoadEntity(r *http.Request) (interface{}, error) {
	return LoadEntityFromUrl(r, "id")
}

func mustLoadRevisionsFromStrings(conn database.DB, pid uuid.UUID, idstrs ...string) ([]*PostRevision, error) {
	revisions := make([]interface{}, len(idstrs))
	for i, idstr := range idstrs {
		id, err := uuid.FromString(idstr)
		if err != nil {
			return nil, err
		}
		revisions[i] = id
	}

	placeholders := util.GeneratePlaceholders(2, len(revisions))

	revs, err := listRevisionsByCondition(conn, "post = $1 AND id IN ("+placeholders+")",
		append([]interface{}{pid}, revisions...)...)
	if err != nil {
		return nil, err
	}

	if len(revs) != len(idstrs) {
		return nil, errors.New("not enough returned rows")
	}

	return revs, nil
}
