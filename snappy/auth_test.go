package snappy

import (
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	"launchpad.net/snappy/helpers"

	. "launchpad.net/gocheck"
)

const mockStoreReturnToken = `
{
    "openid": "the-open-id-string-that-is-also-the-consumer-key-in-our-store", 
    "token_name": "some-token-name", 
    "date_updated": "2015-02-27T15:00:55.062", 
    "token_key": "the-token-key", 
    "consumer_secret": "the-consumer-secret", 
    "href": "/api/v2/tokens/oauth/something", 
    "date_created": "2015-02-27T14:54:30.863", 
    "consumer_key": "the-consumer-key", 
    "token_secret": "the-token-secret"
}
`

func (s *SnapTestSuite) TestRequestStoreToken(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, mockStoreReturnToken)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()
	ubuntuoneOauthAPI = mockServer.URL + "/token/oauth"

	token, err := RequestStoreToken("guy@example.com", "passwd", "some-token-name", "")
	c.Assert(err, IsNil)
	c.Assert(token.TokenKey, Equals, "the-token-key")
	c.Assert(token.TokenSecret, Equals, "the-token-secret")
	c.Assert(token.ConsumerSecret, Equals, "the-consumer-secret")
	c.Assert(token.ConsumerKey, Equals, "the-consumer-key")
}

func (s *SnapTestSuite) TestWriteStoreToken(c *C) {
	os.Setenv("HOME", s.tempdir)
	mockStoreToken := StoreToken{TokenName: "meep"}
	err := WriteStoreToken(mockStoreToken)

	c.Assert(err, IsNil)
	outFile := filepath.Join(s.tempdir, "apps", "snappy", "auth", "sso.json")
	c.Assert(helpers.FileExists(outFile), Equals, true)
	content, err := ioutil.ReadFile(outFile)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, `{
 "openid": "",
 "token_name": "meep",
 "date_updated": "",
 "date_created": "",
 "href": "",
 "token_key": "",
 "token_secret": "",
 "consumer_secret": "",
 "consumer_key": ""
}`)
}

func (s *SnapTestSuite) TestReadStoreToken(c *C) {
	os.Setenv("HOME", s.tempdir)
	mockStoreToken := StoreToken{TokenName: "meep"}
	err := WriteStoreToken(mockStoreToken)
	c.Assert(err, IsNil)

	readToken, err := ReadStoreToken()
	c.Assert(err, IsNil)
	c.Assert(readToken.TokenName, Equals, "meep")
}
