/*
 * Copyright (C) 2014-2015 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */
package snappy

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"time"

	. "launchpad.net/gocheck"
)

/* acquired via:
   curl https://system-image.ubuntu.com/ubuntu-core/devel/generic_armhf/index.json
*/
const mockSystemImageIndexJSONTemplate = `{
    "global": {
        "generated_at": "Thu Feb 19 18:26:23 UTC 2015"
    },
    "images": [
        {
            "description": ",version=1",
            "files": [
                {
                    "checksum": "3869be55a95db880862fe3dc3f5d643101736f674e9970a2099a883bd2bd2367",
                    "order": 0,
                    "path": "/pool/ubuntu-3f8ef532557aaec57264565b9948734bfe01f2a39886c816b0f69ae19e25f903.tar.xz",
                    "signature": "/pool/ubuntu-3f8ef532557aaec57264565b9948734bfe01f2a39886c816b0f69ae19e25f903.tar.xz.asc",
                    "size": 71081252
                },
                {
                    "checksum": "f1a5ae7b4264e045ad58710a427168c02732e29130e5619dd2a82bbc8781b7c0",
                    "order": 1,
                    "path": "/pool/device-bf0a49e75a3deb99855f186906f17d7668e2dcc3a0b38f5feade3e6f7c75e9b8.tar.xz",
                    "signature": "/pool/device-bf0a49e75a3deb99855f186906f17d7668e2dcc3a0b38f5feade3e6f7c75e9b8.tar.xz.asc",
                    "size": 52589508
                },
                {
                    "checksum": "a84fb49b505a797e1ddd6811d8cf79b5fe2a021198cd221e022318026dfb5417",
                    "order": 2,
                    "path": "/ubuntu-core/devel/generic_armhf/version-1.tar.xz",
                    "signature": "/ubuntu-core/devel/generic_armhf/version-1.tar.xz.asc",
                    "size": 328
                }
            ],
            "type": "full",
            "version": 1,
            "version_detail": ",version=1"
        },
        {
            "description": ",version=2",
            "files": [
                {
                    "checksum": "8be1d2c82a6d785089de91febf70aa7aa790c23fae4f2e02c5dc4dfc343d005a",
                    "order": 0,
                    "path": "/pool/ubuntu-0e6f7a24f941a2fbe27c922b5158da9ab177afaa851c28c002a22f4166f3ec01.tar.xz",
                    "signature": "/pool/ubuntu-0e6f7a24f941a2fbe27c922b5158da9ab177afaa851c28c002a22f4166f3ec01.tar.xz.asc",
                    "size": 70576648
                },
                {
                    "checksum": "f1a5ae7b4264e045ad58710a427168c02732e29130e5619dd2a82bbc8781b7c0",
                    "order": 1,
                    "path": "/pool/device-bf0a49e75a3deb99855f186906f17d7668e2dcc3a0b38f5feade3e6f7c75e9b8.tar.xz",
                    "signature": "/pool/device-bf0a49e75a3deb99855f186906f17d7668e2dcc3a0b38f5feade3e6f7c75e9b8.tar.xz.asc",
                    "size": 52589508
                },
                {
                    "checksum": "242f0198b4bd0b63c943e7ff7eb46889753c725cb5b127a7bb76600bcecee544",
                    "order": 2,
                    "path": "/ubuntu-core/devel/generic_armhf/version-2.tar.xz",
                    "signature": "/ubuntu-core/devel/generic_armhf/version-2.tar.xz.asc",
                    "size": 332
                }
            ],
            "type": "full",
            "version": %s,
            "version_detail": ",version=2"
        }
    ]
}`

var mockSystemImageIndexJSON = fmt.Sprintf(mockSystemImageIndexJSONTemplate, "2")

func runMockSystemImageWebServer() *httptest.Server {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, mockSystemImageIndexJSON)
	}))
	if mockServer == nil {
		return nil
	}
	systemImageServer = mockServer.URL
	return mockServer
}

func (s *SITestSuite) TestCheckForUpdates(c *C) {
	mockConfigFile := filepath.Join(c.MkDir(), "channel.init")
	makeFakeSystemImageChannelConfig(c, mockConfigFile, "1")
	updateStatus, err := systemImageClientCheckForUpdates(mockConfigFile)
	c.Assert(err, IsNil)
	c.Assert(updateStatus.targetVersion, Equals, "2")
	c.Assert(updateStatus.targetVersionDetails, Equals, ",version=2")
	c.Assert(updateStatus.lastUpdate, Equals, time.Date(2015, 02, 19, 18, 26, 23, 0, time.UTC))
}
