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

package account_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tamasd/simplesite/util/testutil"
)

func TestRegistrationAndLogin(t *testing.T) {
	srv := testutil.SetupTestSiteFromEnv()
	defer srv.Cleanup()
	c := srv.CreateClient(t)

	regdata := testutil.TestRegData()
	c.RegistrationAndLogin(regdata)
	c.FollowRedirect()

	resp := c.ClickLink("li.logout a")
	require.Equal(t, http.StatusFound, resp.StatusCode)
}
