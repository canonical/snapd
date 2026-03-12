package userd_test

import (
	"fmt"
	"os"
	"path"
	"slices"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/usersession/userd"
)

type userdSuite struct {
	testutil.BaseTest
}

var _ = Suite(&userdSuite{})

func createFakeUnitService(folder, serviceName, wantedBy string) (string, error) {
	filepath := path.Join(folder, serviceName+".service")
	err := os.WriteFile(filepath, []byte(fmt.Sprintf(`[Unit]
Description=%s

[Install]
WantedBy=%s
`, serviceName, wantedBy)), 0644)
	return filepath, err
}

func (s *userdSuite) TestCheckServicePlacementNoChange(c *C) {
	folder := c.MkDir()
	testServicePath, err := createFakeUnitService(folder, "test", "graphical-session.target")
	c.Assert(err, IsNil)

	reenabledServiceName := ""
	reenabledService := false
	oldfunc := userd.MockReenableUserService(func(service string) error {
		reenabledServiceName = service
		reenabledService = true
		return nil
	})
	defer oldfunc()

	err = userd.CheckServicePlacement("graphical-session.target", testServicePath)
	c.Assert(err, IsNil)
	c.Assert(reenabledService, Equals, false)
	c.Assert(reenabledServiceName, Equals, "")
}

func (s *userdSuite) TestCheckServicePlacementWithChange(c *C) {
	folder := c.MkDir()
	testServicePath, err := createFakeUnitService(folder, "test", "graphical-session.target")
	c.Assert(err, IsNil)

	reenabledServiceName := ""
	reenabledService := false
	oldfunc := userd.MockReenableUserService(func(service string) error {
		reenabledServiceName = service
		reenabledService = true
		return nil
	})
	defer oldfunc()

	err = userd.CheckServicePlacement("default.target", testServicePath)
	c.Assert(err, IsNil)
	c.Assert(reenabledService, Equals, true)
	c.Assert(reenabledServiceName, Equals, "test.service")
}

func (s *userdSuite) TestSanitizeUserServices(c *C) {
	fakeHomeFolder := c.MkDir()
	oldfuncHome := userd.MockUserHomeDir(func() (string, error) {
		return fakeHomeFolder, nil
	})
	reenabledServices := []string{}
	defer oldfuncHome()
	oldfuncReenable := userd.MockReenableUserService(func(service string) error {
		reenabledServices = append(reenabledServices, service)
		return nil
	})
	defer oldfuncReenable()

	userSystemdPath := path.Join(fakeHomeFolder, ".config", "systemd", "user")
	defaultWanted := path.Join(userSystemdPath, "default.target.wants")
	graphicalWanted := path.Join(userSystemdPath, "graphical-session.target.wants")
	err := os.MkdirAll(defaultWanted, 0755)
	c.Assert(err, IsNil)
	err = os.MkdirAll(graphicalWanted, 0755)
	c.Assert(err, IsNil)

	service1Path, err := createFakeUnitService(defaultWanted, "snap.test1", "default.target")
	c.Assert(err, IsNil)
	_, err = createFakeUnitService(defaultWanted, "snap.test2", "graphical-session.target")
	c.Assert(err, IsNil)
	_, err = createFakeUnitService(graphicalWanted, "snap.test3", "default.target")
	c.Assert(err, IsNil)
	_, err = createFakeUnitService(graphicalWanted, "snap.test4", "graphical-session.target")
	c.Assert(err, IsNil)

	_, err = createFakeUnitService(defaultWanted, "test5", "default.target")
	c.Assert(err, IsNil)
	_, err = createFakeUnitService(defaultWanted, "test6", "graphical-session.target")
	c.Assert(err, IsNil)
	_, err = createFakeUnitService(graphicalWanted, "test7", "default.target")
	c.Assert(err, IsNil)
	_, err = createFakeUnitService(graphicalWanted, "test8", "graphical-session.target")
	c.Assert(err, IsNil)

	// soft link to an existing file
	softlink1 := path.Join(defaultWanted, "snap.test9.service")
	softlink2 := path.Join(defaultWanted, "snap.test10.service")
	err = os.Symlink(service1Path, softlink1)
	c.Assert(err, IsNil)
	err = os.Symlink(service1Path+".not-exist", softlink2)
	c.Assert(err, IsNil)

	_, err = os.Lstat(softlink1)
	c.Assert(err, IsNil)
	_, err = os.Lstat(softlink2)
	c.Assert(err, IsNil)
	_, err = os.Stat(softlink1)
	c.Assert(err, IsNil)
	_, err = os.Stat(softlink2)
	c.Assert(err, NotNil)

	userd.SanitizeUserServices()
	c.Assert(len(reenabledServices), Equals, 2)
	c.Assert(slices.Contains(reenabledServices, "snap.test2.service"), Equals, true)
	c.Assert(slices.Contains(reenabledServices, "snap.test3.service"), Equals, true)
	_, err = os.Lstat(softlink1)
	c.Assert(err, IsNil)
	_, err = os.Stat(softlink1)
	c.Assert(err, IsNil)
	_, err = os.Lstat(softlink2)
	c.Assert(err, NotNil)
}
