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
	"encoding/hex"
	"strings"
	"unicode"

	"github.com/pkg/errors"
	uuid "github.com/satori/go.uuid"
	"github.com/tamasd/simplesite/database"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Account represents the main user entity.
type Account struct {
	ID       uuid.UUID `json:"id"`
	Username string    `json:"username"`
	Email    string    `json:"email"`
	Active   bool      `json:"active"`

	password string
	salt     string
}

// SchemaSQL returns the schema of the account entity.
func (a Account) SchemaSQL() string {
	return `
		CREATE TABLE account (
			id UUID NOT NULL,
			username VARCHAR(255) NOT NULL,
			password VARCHAR(128) NOT NULL,
			salt VARCHAR(32) NOT NULL,
			email VARCHAR(255) NOT NULL,
			active BOOLEAN NOT NULL,
			normalized_username VARCHAR(255) NOT NULL,
			PRIMARY KEY (id)
		);
	
		CREATE UNIQUE INDEX ON account (username);
		CREATE UNIQUE INDEX ON account (salt);
		CREATE UNIQUE INDEX ON account (email);
		CREATE UNIQUE INDEX ON account (normalized_username);
	`
}

// Save updates or inserts the account into the database.
func (a *Account) Save(conn database.DB) error {
	if uuid.Equal(a.ID, uuid.Nil) {
		a.ID = uuid.NewV4()
	}

	_, err := conn.Exec(`
		INSERT INTO account (id, username, password, salt, email, active, normalized_username)
		VALUES($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id)
		DO UPDATE SET
			username = $2,
			password = $3,
			salt = $4,
			email = $5,
			active = $6,
			normalized_username = $7
	`,
		a.ID,
		a.Username,
		a.password,
		a.salt,
		a.Email,
		a.Active,
		NormalizeAccountname(a.Username),
	)
	return errors.Wrap(err, "error saving account")
}

// SetPassword sets a password on the account by correctly hashing it and
// updating the salt.
func (a *Account) SetPassword(pw string) {
	pass, salt := HashPassword(pw, nil)
	a.password = hex.EncodeToString(pass)
	a.salt = hex.EncodeToString(salt)
}

// CheckPassword compares a given password with the saved one.
func (a *Account) CheckPassword(pw string) bool {
	pass, _ := hex.DecodeString(a.password)
	salt, _ := hex.DecodeString(a.salt)
	hash, _ := HashPassword(pw, salt)

	return CompareHashes(pass, hash)
}

// LoadAccount loads an account from the database by a given id.
func LoadAccount(conn database.DB, id uuid.UUID) (*Account, error) {
	return loadAccountByCondition(conn, "id = $1", id)
}

// LoadAccountByUsername loads an account from the database by a given username.
func LoadAccountByUsername(conn database.DB, username string) (*Account, error) {
	return loadAccountByCondition(conn, "username = $1", username)
}

// LoadAccountByEmail loads an account from the database by a given email.
func LoadAccountByEmail(conn database.DB, email string) (*Account, error) {
	return loadAccountByCondition(conn, "email = $1", email)
}

func loadAccountByCondition(conn database.DB, condition string, args ...interface{}) (*Account, error) {
	a := &Account{}
	err := conn.QueryRow(`
		SELECT id, username, password, salt, email, active
		FROM account
		WHERE `+condition+`
	`, args...).Scan(
		&a.ID,
		&a.Username,
		&a.password,
		&a.salt,
		&a.Email,
		&a.Active,
	)
	if err != nil {
		return nil, err
	}

	return a, nil
}

// NormalizeAccountname creates a normalized version of the account name.
//
// The purpose of this function is to make it harder to create misleading
// usernames, that look the same but different (because of fancy unicode
// characters, separators, lower/upper case differences).
func NormalizeAccountname(accountname string) string {
	accountname = strings.ToLower(accountname)
	for _, sep := range Separators {
		accountname = strings.Replace(accountname, sep, "", -1)
	}

	t := transform.Chain(norm.NFKD, runes.Remove(runes.In(unicode.Mn)), norm.NFKC)
	accountname, _, _ = transform.String(t, accountname)

	return accountname
}

// IsAccountnameBlacklisted checks if the account name is on the internal
// blacklist.
//
// Currently uses an O(n) lookup, but it should be fine, given that the
// blacklist only has around ~100~200 items.
func IsAccountnameBlacklisted(accountname string) bool {
	for _, b := range AccountnameBlacklist {
		if accountname == b {
			return true
		}
	}

	return false
}

// Separators is a list of common separators in usernames.
var Separators = []string{
	" ",
	"	",
	".",
	"-",
	"_",
}

// AccountnameBlacklist is a list of account names that can't be registered.
//
// This list is copied from django-registration.
var AccountnameBlacklist = []string{
	// Hostnames with special/reserved meaning.
	"autoconfig",    // Thunderbird autoconfig
	"autodiscover",  // MS Outlook/Exchange autoconfig
	"broadcasthost", // Network broadcast hostname
	"isatap",        // IPv6 tunnel autodiscovery
	"localdomain",   // Loopback
	"localhost",     // Loopback
	"wpad",          // Proxy autodiscovery

	// Common protocol hostnames.
	"ftp",
	"imap",
	"mail",
	"news",
	"pop",
	"pop3",
	"smtp",
	"usenet",
	"uucp",
	"webmail",
	"www",

	// Email addresses known used by certificate authorities during
	// verification.
	"admin",
	"administrator",
	"hostmaster",
	"info",
	"is",
	"it",
	"mis",
	"postmaster",
	"root",
	"ssladmin",
	"ssladministrator",
	"sslwebmaster",
	"sysadmin",
	"webmaster",

	// RFC-2142-defined names not already covered.
	"abuse",
	"marketing",
	"noc",
	"sales",
	"security",
	"support",

	// Common no-reply email addresses.
	"mailer-daemon",
	"nobody",
	"noreply",
	"no-reply",

	// Sensitive filenames.
	"clientaccesspolicy.xml", // Silverlight cross-domain policy file.
	"crossdomain.xml",        // Flash cross-domain policy file.
	"favicon.ico",
	"humans.txt",
	"keybase.txt", // Keybase ownership-verification URL.
	"robots.txt",
	".htaccess",
	".htpasswd",

	// Other names which could be problems depending on URL/subdomain
	// structure.
	"account",
	"accounts",
	"blog",
	"buy",
	"clients",
	"contact",
	"contactus",
	"contact-us",
	"copyright",
	"dashboard",
	"doc",
	"docs",
	"download",
	"downloads",
	"enquiry",
	"faq",
	"help",
	"inquiry",
	"license",
	"login",
	"logout",
	"me",
	"myaccount",
	"payments",
	"plans",
	"portfolio",
	"preferences",
	"pricing",
	"privacy",
	"profile",
	"register",
	"secure",
	"settings",
	"signin",
	"signup",
	"ssl",
	"status",
	"subscribe",
	"terms",
	"tos",
	"user",
	"users",
	"weblog",
	"work",

	".well-known",
}
