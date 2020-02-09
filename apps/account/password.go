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
	"crypto/rand"

	"golang.org/x/crypto/argon2"
)

// HashPassword hashes a string password.
//
// If the salt is nil, it will be generated.
//
// Returns the hash and salt.
func HashPassword(password string, salt []byte) ([]byte, []byte) {
	if salt == nil {
		salt = make([]byte, 16)
		if _, err := rand.Read(salt); err != nil {
			panic(err)
		}
	}

	return argon2.IDKey([]byte(password), salt, 1, 64*1024, 4, 32), salt
}

// CompareHashes safely compares password hashes.
func CompareHashes(h0, h1 []byte) bool {
	if len(h0) != len(h1) {
		return false
	}

	result := true
	for i := 0; i < len(h0); i++ {
		result = result && h0[i] == h1[i]
	}

	return result
}
