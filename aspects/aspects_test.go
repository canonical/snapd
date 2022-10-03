package aspects_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/aspects"
	"github.com/snapcore/snapd/testutil"
)

type aspectSuite struct {
}

func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&aspectSuite{})

func (*aspectSuite) TestAspectDirectory(c *C) {
	aspectDir, err := aspects.NewAspectDirectory("system/network", map[string]interface{}{
		"wifi-setup": []map[string]string{
			{"name": "ssids", "path": "wifi.ssids"},
			{"name": "ssid", "path": "wifi.ssid"},
			{"name": "top-level", "path": "top-level"},
		},
	}, aspects.NewStorage(), aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	wsAspect := aspectDir.Aspect("wifi-setup")
	err = wsAspect.Set("ssid", "my-ssid")
	c.Assert(err, IsNil)

	err = wsAspect.Set("ssids", []string{"one", "two"})
	c.Assert(err, IsNil)

	var ssid string
	err = wsAspect.Get("ssid", &ssid)
	c.Assert(err, IsNil)
	c.Check(ssid, Equals, "my-ssid")

	var ssids []string
	err = wsAspect.Get("ssids", &ssids)
	c.Assert(err, IsNil)
	c.Check(ssids, DeepEquals, []string{"one", "two"})

	var topLevel string
	err = wsAspect.Get("top-level", &topLevel)
	c.Assert(err, testutil.ErrorIs, &aspects.ErrNotFound{})

	err = wsAspect.Set("top-level", "randomValue")
	c.Assert(err, IsNil)

	err = wsAspect.Get("top-level", &topLevel)
	c.Assert(err, IsNil)
	c.Check(topLevel, Equals, "randomValue")
}
