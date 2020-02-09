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

package database

import (
	"database/sql"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/tamasd/simplesite/respond"
	"github.com/tamasd/simplesite/server"
	"github.com/tamasd/simplesite/util"
	"github.com/urfave/negroni"
)

const (
	dbContextKey = "conn"
)

var (
	spaces = regexp.MustCompile(`\s+`)
)

// Get returns the database from the request context.
func Get(r *http.Request) DB {
	return r.Context().Value(dbContextKey).(DB)
}

// GetTx returns the transaction from the request context.
func GetTx(r *http.Request) Transaction {
	return r.Context().Value(dbContextKey).(Transaction)
}

// MaybeRollback tries to roll back a transaction.
//
// If the current connection is not a transaction, or if the transaction is
// already done then nothing will happen.
func MaybeRollback(r *http.Request) error {
	if tx, ok := Get(r).(Transaction); ok {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			return err
		}
	}

	return nil
}

// DatabaseEntity represents an entity that has schema in the database.
type DatabaseEntity interface {
	SchemaSQL() string
}

// Ensure makes sure that a given DatabaseEntity has its schema in the
// database.
func Ensure(logger logrus.FieldLogger, conn DB, v DatabaseEntity) error {
	t := reflect.TypeOf(v)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	tablename := util.ToSnakeCase(t.Name())
	logger = logger.WithField("tablename", tablename)
	logger.Debugln("determined table name")
	exists, err := tableExists(conn, tablename)
	if err != nil {
		return errors.Wrap(err, "error checking if table exists")
	}

	if exists {
		logger.Debugln("table exists, skipping")
		return nil
	}

	schema := v.SchemaSQL()
	logger.WithField("schema", schema).Debugln("creating schema")
	_, err = conn.Exec(schema)
	return err
}

func tableExists(conn DB, tablename string) (bool, error) {
	var exists bool
	err := conn.QueryRow(`
		SELECT EXISTS (
			SELECT 1 
			FROM   pg_catalog.pg_class c
			WHERE  c.relname = $1
			AND    c.relkind = 'r'
		);
	`, tablename).Scan(&exists)

	return exists, err
}

// DB represents a database connection.
type DB interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
	Query(query string, args ...interface{}) (*sql.Rows, error)
	QueryRow(query string, args ...interface{}) *sql.Row
}

// Transaction represents a database connection with an active transaction.
type Transaction interface {
	DB
	Commit() error
	Rollback() error
}

// TransactionFactory can initiate a transaction.
type TransactionFactory interface {
	Begin() (Transaction, error)
}

type dbWrapper struct {
	*sql.DB
}

func (w *dbWrapper) Begin() (Transaction, error) {
	return w.DB.Begin()
}

type loggerDB struct {
	logger logrus.FieldLogger
	db     DB
}

// NewLoggerDB wraps a database connection with a logger.
func NewLoggerDB(logger logrus.FieldLogger, db DB) DB {
	ldb := loggerDB{
		logger: logger,
		db:     db,
	}

	if _, ok := db.(Transaction); ok {
		return &transactionLoggerDB{ldb}
	}
	if _, ok := db.(TransactionFactory); ok {
		return &transactionFactoryLoggerDB{ldb}
	}

	return &ldb
}

func (d *loggerDB) Exec(query string, args ...interface{}) (sql.Result, error) {
	start := time.Now()
	res, err := d.db.Exec(query, args...)
	logger := d.logger.WithFields(logrus.Fields{
		"query":    cleanSQL(query),
		"args":     args,
		"duration": time.Since(start),
	})
	if err != nil {
		logger = logger.WithError(err)
	}
	logger.Traceln("executing query")
	return res, err
}

func (d *loggerDB) Query(query string, args ...interface{}) (*sql.Rows, error) {
	start := time.Now()
	rows, err := d.db.Query(query, args...)
	logger := d.logger.WithFields(logrus.Fields{
		"query":    cleanSQL(query),
		"args":     args,
		"duration": time.Since(start),
	})
	if err != nil {
		logger = logger.WithError(err)
	}
	logger.Traceln("running query")

	return rows, err
}

func (d *loggerDB) QueryRow(query string, args ...interface{}) *sql.Row {
	start := time.Now()
	row := d.db.QueryRow(query, args...)
	d.logger.WithFields(logrus.Fields{
		"query":    cleanSQL(query),
		"args":     args,
		"duration": time.Since(start),
	}).Traceln("running query row")

	return row
}

type transactionFactoryLoggerDB struct {
	loggerDB
}

func (d *transactionFactoryLoggerDB) Begin() (Transaction, error) {
	const msg = "begin transaction"
	if f, ok := d.db.(TransactionFactory); ok {
		start := time.Now()
		tx, err := f.Begin()
		logger := d.logger.WithFields(logrus.Fields{
			"transaction-id": util.RandomHexString(8),
			"duration":       time.Since(start),
		})
		if err != nil {
			logger = logger.WithError(err)
			logger.Traceln(msg)
			return nil, err
		}
		logger.Traceln(msg)

		return NewLoggerDB(logger, tx).(Transaction), nil
	}

	return nil, nil
}

type transactionLoggerDB struct {
	loggerDB
}

func (d *transactionLoggerDB) Commit() error {
	start := time.Now()
	err := d.db.(Transaction).Commit()
	logger := d.logger.WithFields(logrus.Fields{
		"duration": time.Since(start),
	})
	if err != nil && err != sql.ErrTxDone {
		logger = logger.WithError(err)
	}
	logger.Traceln("commit transaction")

	return err
}

func (d *transactionLoggerDB) Rollback() error {
	start := time.Now()
	err := d.db.(Transaction).Rollback()
	logger := d.logger.WithFields(logrus.Fields{
		"duration": time.Since(start),
	})
	if err != nil && err != sql.ErrTxDone {
		logger = logger.WithError(err)
	}
	logger.Traceln("rollback transaction")

	return err
}

// Connect creates a database connection to a PostgreSQL database.
func Connect(dbUrl string) (DB, error) {
	conn, err := sql.Open("postgres", dbUrl)
	if err != nil {
		return nil, err
	}

	return &dbWrapper{
		DB: conn,
	}, nil
}

// Middleware stores a database connection in the request context.
type Middleware struct {
	conn DB
}

func NewMiddleware(conn DB) *Middleware {
	return &Middleware{
		conn: conn,
	}
}

func (m *Middleware) ServeHTTP(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	r = util.SetContext(r, dbContextKey, m.conn)
	next.ServeHTTP(w, r)
}

// TxMiddleware stores a database transaction in the request context.
type TxMiddleware struct {
	auto bool
}

// NewTxMiddleware creates a TxMiddleware.
//
// The auto parameter tells the middleware to automatically commit or roll back
// the transaction based on the response code (roll back over 400, commit
// below).
func NewTxMiddleware(auto bool) *TxMiddleware {
	return &TxMiddleware{
		auto: auto,
	}
}

func (m *TxMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	logger := server.GetLogger(r)
	tx, err := maybeBegin(Get(r))
	if err != nil || tx == nil {
		logger.Errorln("transaction failed")
		respond.Error(w, r, http.StatusInternalServerError, "database error", nil, err)
		return
	}

	if m.auto {
		defer func() {
			if err = tx.Rollback(); err != nil && err != sql.ErrTxDone {
				logger.WithError(err).Errorln("failed to roll back transaction")
			}
		}()
	}

	r = util.SetContext(r, dbContextKey, tx)

	next.ServeHTTP(w, r)

	if m.auto {
		status := w.(negroni.ResponseWriter).Status()
		if status < 400 {
			if err = tx.Commit(); err != nil && err != sql.ErrTxDone {
				logger.WithError(err).Errorln("failed to commit transaction")
			}
		}
	}
}

func maybeBegin(conn DB) (Transaction, error) {
	if f, ok := conn.(TransactionFactory); ok {
		return f.Begin()
	}

	return nil, nil
}

func cleanSQL(query string) string {
	return spaces.ReplaceAllString(strings.TrimSpace(query), " ")
}
