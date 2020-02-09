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

package mailer

import "net/smtp"

// Mailer lets the application send emails.
type Mailer interface {
	From() string
	Send(to []string, msg []byte) error
}

// SMTP is the default implementation of Mailer.
type SMTP struct {
	from string
	addr string
	auth smtp.Auth
}

func NewSMTP(from, addr string, auth smtp.Auth) *SMTP {
	return &SMTP{
		from: from,
		addr: addr,
		auth: auth,
	}
}

func (m *SMTP) From() string {
	return m.from
}

func (m *SMTP) Send(to []string, msg []byte) error {
	return smtp.SendMail(m.addr, m.auth, m.From(), to, msg)
}
