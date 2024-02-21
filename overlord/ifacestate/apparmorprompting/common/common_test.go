package common_test

import (
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	doublestar "github.com/bmatcuk/doublestar/v4"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting/common"
	"github.com/snapcore/snapd/sandbox/apparmor/notify"
)

func Test(t *testing.T) { TestingT(t) }

type commonSuite struct {
	tmpdir string
}

var _ = Suite(&commonSuite{})

func (s *commonSuite) SetUpTest(c *C) {
	s.tmpdir = c.MkDir()
	dirs.SetRootDir(s.tmpdir)
}

func (s *commonSuite) TestConstraintsValidateForInterface(c *C) {
	cases := []struct {
		iface   string
		pattern string
		perms   []string
		errStr  string
	}{
		{
			"foo",
			"invalid/path",
			[]string{"read"},
			"constraints incompatible with the given interface.*",
		},
		{
			"home",
			"invalid/path",
			[]string{"read"},
			"invalid path pattern.*",
		},
		{
			"camera",
			"/valid/path",
			[]string{"invalid"},
			"unsupported permission.*",
		},
		{
			"home",
			"/valid/path",
			[]string{},
			fmt.Sprintf("%v", common.ErrPermissionsListEmpty),
		},
	}
	for _, testCase := range cases {
		constraints := &common.Constraints{
			PathPattern: testCase.pattern,
			Permissions: testCase.perms,
		}
		err := constraints.ValidateForInterface(testCase.iface)
		c.Check(err, ErrorMatches, testCase.errStr)
	}
}

func (*commonSuite) TestConstraintsMatch(c *C) {
	cases := []struct {
		pattern string
		path    string
		matches bool
	}{
		{
			"/home/test/Documents/foo.txt",
			"/home/test/Documents/foo.txt",
			true,
		},
		{
			"/home/test/Documents/foo",
			"/home/test/Documents/foo.txt",
			false,
		},
		{
			"/home/test/Documents",
			"/home/test/Documents",
			true,
		},
		{
			"/home/test/Documents/*",
			"/home/test/Documents",
			true,
		},
		{
			"/home/test/Documents/**",
			"/home/test/Documents",
			true,
		},
		{
			"/home/test/Documents/*",
			"/home/test/Documents/foo.txt",
			true,
		},
		{
			"/home/test/Documents/**",
			"/home/test/Documents/foo.txt",
			true,
		},
		{
			"/home/test/Documents/**/*.txt",
			"/home/test/Documents/foo.txt",
			true,
		},
		{
			"/home/test/Documents/**/*.txt",
			"/home/test/Documents/foo/bar.tar.gz",
			false,
		},
		{
			"/home/test/Documents/**",
			"/home/test/Documents/foo/bar.tar.gz",
			true,
		},
		{
			"/home/test/Documents/**/*.gz",
			"/home/test/Documents/foo/bar.tar.gz",
			true,
		},
		{
			"/home/test/Documents/**/*.tar.gz",
			"/home/test/Documents/foo/bar.tar.gz",
			true,
		},
		{
			"/home/test/Documents/*.tar.gz",
			"/home/test/Documents/foo/bar.tar.gz",
			false,
		},
		{
			"/home/test/Documents/*",
			"/home/test/Documents/foo/bar.tar.gz",
			false,
		},
		{
			"/home/test/**",
			"/home/test/Documents/foo/bar.tar.gz",
			true,
		},
		{
			"/home/test/*",
			"/home/test/Documents/foo/bar.tar.gz",
			false,
		},
		{
			"/home/test/**/*.tar.gz",
			"/home/test/Documents/foo/bar.tar.gz",
			true,
		},
		{
			"/home/test/**/*.gz",
			"/home/test/Documents/foo/bar.tar.gz",
			true,
		},
		{
			"/home/test/**/*.txt",
			"/home/test/Documents/foo/bar.tar.gz",
			false,
		},
	}
	for _, testCase := range cases {
		constraints := &common.Constraints{
			PathPattern: testCase.pattern,
			Permissions: []string{"read"},
		}
		result, err := constraints.Match(testCase.path)
		c.Check(err, IsNil, Commentf("test case: %+v", testCase))
		c.Check(result, Equals, testCase.matches, Commentf("test case: %+v", testCase))
	}
}

func (s *commonSuite) TestConstraintsMatchUnhappy(c *C) {
	badPath := `bad\pattern\`
	badConstraints := &common.Constraints{
		PathPattern: badPath,
		Permissions: []string{"read"},
	}
	matches, err := badConstraints.Match(badPath)
	c.Check(err, Equals, doublestar.ErrBadPattern)
	c.Check(matches, Equals, false)
}

func (s *commonSuite) TestConstraintsRemovePermission(c *C) {
	cases := []struct {
		initial []string
		remove  string
		final   []string
		err     error
	}{
		{
			[]string{"read", "write", "execute"},
			"read",
			[]string{"write", "execute"},
			nil,
		},
		{
			[]string{"read", "write", "execute"},
			"write",
			[]string{"read", "execute"},
			nil,
		},
		{
			[]string{"read", "write", "execute"},
			"execute",
			[]string{"read", "write"},
			nil,
		},
		{
			[]string{"read", "write", "read"},
			"read",
			[]string{"write"},
			nil,
		},
		{
			[]string{"read"},
			"read",
			[]string{},
			nil,
		},
		{
			[]string{"read", "read"},
			"read",
			[]string{},
			nil,
		},
		{
			[]string{"read", "write", "execute"},
			"append",
			[]string{"read", "write", "execute"},
			common.ErrPermissionNotInList,
		},
		{
			[]string{},
			"read",
			[]string{},
			common.ErrPermissionNotInList,
		},
	}
	for _, testCase := range cases {
		constraints := &common.Constraints{
			PathPattern: "/path/to/foo",
			Permissions: testCase.initial,
		}
		err := constraints.RemovePermission(testCase.remove)
		c.Check(err, Equals, testCase.err)
		c.Check(constraints.Permissions, DeepEquals, testCase.final)
	}
}

func (s *commonSuite) TestConstraintsContainPermissions(c *C) {
	cases := []struct {
		constPerms []string
		queryPerms []string
		contained  bool
	}{
		{
			[]string{"read", "write", "execute"},
			[]string{"read", "write", "execute"},
			true,
		},
		{
			[]string{"execute", "write", "read"},
			[]string{"read", "write", "execute"},
			true,
		},
		{
			[]string{"read", "write", "execute"},
			[]string{"read"},
			true,
		},
		{
			[]string{"read", "write", "execute"},
			[]string{"execute"},
			true,
		},
		{
			[]string{"read", "write", "execute"},
			[]string{"read", "write", "execute", "append"},
			false,
		},
		{
			[]string{"read", "write", "execute"},
			[]string{"read", "append"},
			false,
		},
		{
			[]string{"foo", "bar", "baz"},
			[]string{"foo", "bar"},
			true,
		},
		{
			[]string{"foo", "bar", "baz"},
			[]string{"fizz", "buzz"},
			false,
		},
	}
	for _, testCase := range cases {
		constraints := &common.Constraints{
			PathPattern: "arbitrary",
			Permissions: testCase.constPerms,
		}
		contained := constraints.ContainPermissions(testCase.queryPerms)
		c.Check(contained, Equals, testCase.contained, Commentf("testCase: %+v", testCase))
	}
}

func (s *commonSuite) TestOutcomeAsBool(c *C) {
	result, err := common.OutcomeAllow.AsBool()
	c.Check(err, IsNil)
	c.Check(result, Equals, true)
	result, err = common.OutcomeDeny.AsBool()
	c.Check(err, IsNil)
	c.Check(result, Equals, false)
	_, err = common.OutcomeUnset.AsBool()
	c.Check(err, Equals, common.ErrInvalidOutcome)
	_, err = common.OutcomeType("foo").AsBool()
	c.Check(err, Equals, common.ErrInvalidOutcome)
}

func (s *commonSuite) TestTimestampToTime(c *C) {
	t1, err := common.TimestampToTime("2004-10-20T10:04:05.999999999-05:00")
	c.Assert(err, IsNil)
	c.Check(t1.UTC(), Equals, time.Date(2004, time.October, 20, 15, 4, 5, 999999999, time.UTC))
	c.Check(t1.UTC(), Not(Equals), time.Date(2004, time.October, 20, 10, 4, 5, 999999999, time.UTC))
	_, err = common.TimestampToTime("2004-10-20")
	c.Assert(err, NotNil)
}

func (s *commonSuite) TestTimestamps(c *C) {
	before := time.Now()
	ts := common.CurrentTimestamp()
	after := time.Now()
	parsedTime, err := common.TimestampToTime(ts)
	c.Assert(err, IsNil)
	c.Assert(parsedTime.After(before), Equals, true)
	c.Assert(parsedTime.Before(after), Equals, true)
}

func (s *commonSuite) TestNewIDAndTimestamp(c *C) {
	before := time.Now()
	id := common.NewID()
	idPaired, timestampPaired := common.NewIDAndTimestamp()
	after := time.Now()
	data1, err := base32.StdEncoding.DecodeString(id)
	c.Assert(err, IsNil)
	data2, err := base32.StdEncoding.DecodeString(idPaired)
	c.Assert(err, IsNil)
	parsedNs := int64(binary.BigEndian.Uint64(data1))
	parsedNsPaired := int64(binary.BigEndian.Uint64(data2))
	parsedTime := time.Unix(parsedNs/1000000000, parsedNs%1000000000)
	parsedTimePaired := time.Unix(parsedNsPaired/1000000000, parsedNsPaired%1000000000)
	c.Assert(parsedTime.After(before), Equals, true)
	c.Assert(parsedTime.Before(after), Equals, true)
	c.Assert(parsedTimePaired.After(before), Equals, true)
	c.Assert(parsedTimePaired.Before(after), Equals, true)
	parsedTimestamp, err := common.TimestampToTime(timestampPaired)
	c.Assert(err, IsNil)
	c.Assert(parsedTimePaired, Equals, parsedTimestamp)
}

func (s *commonSuite) TestLabelToSnapAppHappy(c *C) {
	cases := []struct {
		label string
		snap  string
		app   string
	}{
		{
			label: "snap.nextcloud.occ",
			snap:  "nextcloud",
			app:   "occ",
		},
		{
			label: "snap.lxd.lxc",
			snap:  "lxd",
			app:   "lxc",
		},
		{
			label: "snap.firefox.firefox",
			snap:  "firefox",
			app:   "firefox",
		},
	}
	for _, testCase := range cases {
		snap, app, err := common.LabelToSnapApp(testCase.label)
		c.Check(err, IsNil)
		c.Check(snap, Equals, testCase.snap)
		c.Check(app, Equals, testCase.app)
	}
}

func (s *commonSuite) TestLabelToSnapAppUnhappy(c *C) {
	cases := []string{
		"snap",
		"snap.nextcloud",
		"nextcloud.occ",
		"snap.nextcloud.nextcloud.occ",
		"SNAP.NEXTCLOUD.OCC",
	}
	for _, label := range cases {
		snap, app, err := common.LabelToSnapApp(label)
		c.Check(err, Equals, common.ErrInvalidSnapLabel)
		c.Check(snap, Equals, label)
		c.Check(app, Equals, label)
	}
}

func constructPermissionsMaps() []map[string]map[string]interface{} {
	var permissionsMaps []map[string]map[string]interface{}
	// interfaceFilePermissionsMaps
	filePermissionsMaps := make(map[string]map[string]interface{})
	for iface, permsMap := range common.InterfaceFilePermissionsMaps {
		filePermissionsMaps[iface] = make(map[string]interface{}, len(permsMap))
		for perm, val := range permsMap {
			filePermissionsMaps[iface][perm] = val
		}
	}
	permissionsMaps = append(permissionsMaps, filePermissionsMaps)
	// TODO: do the same for other maps of permissions maps in the future
	return permissionsMaps
}

func (s *commonSuite) TestInterfacesAndPermissionsCompleteness(c *C) {
	permissionsMaps := constructPermissionsMaps()
	// Check that every interface in interfacePriorities is also in
	// interfacePermissionsAvailable and exactly one of the permissions maps.
	// Also, check that the permissions for a given interface in
	// interfacePermissionsAvailable are identical to the permissions in the
	// interface's permissions map.
	// Also, check that each priority only occurs once.
	usedPriorities := make(map[int]bool)
	for iface, priority := range common.InterfacePriorities {
		_, exists := usedPriorities[priority]
		c.Check(exists, Equals, false, Commentf("priority for %s interface is not unique: %d", iface, priority))
		usedPriorities[priority] = true
		perms, err := common.AvailablePermissions(iface)
		c.Check(err, IsNil, Commentf("interface missing from interfacePermissionsAvailable: %s", iface))
		c.Check(perms, Not(HasLen), 0, Commentf("interface has no available permissions: %s", iface))
		found := false
		for _, permsMaps := range permissionsMaps {
			pMap, exists := permsMaps[iface]
			if !exists {
				continue
			}
			c.Check(found, Equals, false, Commentf("interface found in more than one map of interface permissions maps: %s", iface))
			found = true
			// Check that permissions in the list and map are identical
			c.Check(pMap, HasLen, len(perms), Commentf("permissions list and map inconsistent for interface: %s", iface))
			for _, perm := range perms {
				_, exists := pMap[perm]
				c.Check(exists, Equals, true, Commentf("missing permission mapping for %s interface permission: %s", iface, perm))
			}
		}
		if !found {
			c.Errorf("interface not included in any map of interface permissions maps: %s", iface)
		}
	}
	// Check that every interface in interfacePermissionsAvailable is also in
	// interfacePriorities.
	for iface := range common.InterfacePermissionsAvailable {
		_, exists := common.InterfacePriorities[iface]
		c.Check(exists, Equals, true, Commentf("interfacePriorities missing interface from interfacePermissionsAvailable: %s", iface))
	}
	// Check that every interface in one of the permissions maps is also in
	// interfacePriorities.
	for _, permsMaps := range permissionsMaps {
		for iface := range permsMaps {
			_, exists := common.InterfacePriorities[iface]
			c.Check(exists, Equals, true, Commentf("interface not found in any map of permissions maps: %s", iface))
		}
	}
}

func (s *commonSuite) TestInterfaceFilePermissionsMapsCorrectness(c *C) {
	for iface, permsMap := range common.InterfaceFilePermissionsMaps {
		seenPermissions := notify.FilePermission(0)
		for name, mask := range permsMap {
			if duplicate := seenPermissions & mask; duplicate != notify.FilePermission(0) {
				c.Errorf("AppArmor file permission found in more than one permission map for %s interface: %s", iface, duplicate.String())
			}
			c.Check(mask&notify.AA_MAY_OPEN, Equals, notify.FilePermission(0), Commentf("AA_MAY_OPEN may not be included in permissions maps, but %s interface includes it in the map for permission: %s", iface, name))
			seenPermissions |= mask
		}
	}
}

func (s *commonSuite) TestSelectSingleInterface(c *C) {
	defaultInterface := "other"
	fakeInterface := "foo"
	c.Check(common.SelectSingleInterface([]string{}), Equals, defaultInterface, Commentf("input: []string{}"))
	c.Check(common.SelectSingleInterface([]string{""}), Equals, defaultInterface, Commentf(`input: []string{""}`))
	c.Check(common.SelectSingleInterface([]string{fakeInterface}), Equals, defaultInterface, Commentf(`input: []string{""}`))
	for iface := range common.InterfacePriorities {
		c.Check(common.SelectSingleInterface([]string{iface}), Equals, iface)
		fakeList := []string{iface, fakeInterface}
		c.Check(common.SelectSingleInterface(fakeList), Equals, iface)
		fakeList = []string{fakeInterface, iface}
		c.Check(common.SelectSingleInterface(fakeList), Equals, iface)
	}
	c.Check(common.SelectSingleInterface([]string{"home", "camera", "foo"}), Equals, "home")
}

func (s *commonSuite) TestAvailablePermissions(c *C) {
	for iface, perms := range common.InterfacePermissionsAvailable {
		available, err := common.AvailablePermissions(iface)
		c.Check(err, IsNil)
		c.Check(available, DeepEquals, perms)
	}
	available, err := common.AvailablePermissions("foo")
	c.Check(err, ErrorMatches, ".*unsupported interface.*")
	c.Check(available, IsNil)
}

func (s *commonSuite) TestAbstractPermissionsFromAppArmorFilePermissionsHappy(c *C) {
	cases := []struct {
		iface string
		mask  notify.FilePermission
		list  []string
	}{
		{
			"home",
			notify.AA_MAY_READ,
			[]string{"read"},
		},
		{
			"home",
			notify.AA_MAY_WRITE | notify.AA_MAY_APPEND | notify.AA_MAY_CREATE | notify.AA_MAY_DELETE | notify.AA_MAY_RENAME | notify.AA_MAY_CHMOD | notify.AA_MAY_LOCK | notify.AA_MAY_LINK,
			[]string{"write"},
		},
		{
			"home",
			notify.AA_MAY_EXEC | notify.AA_EXEC_MMAP,
			[]string{"execute"},
		},
		{
			"home",
			notify.AA_MAY_OPEN,
			[]string{"read"},
		},
		{
			"home",
			notify.AA_MAY_OPEN | notify.AA_MAY_WRITE,
			[]string{"write"},
		},
		{
			"home",
			notify.AA_MAY_EXEC | notify.AA_MAY_WRITE | notify.AA_MAY_READ,
			[]string{"read", "write", "execute"},
		},
		{
			"camera",
			notify.AA_MAY_WRITE | notify.AA_MAY_READ | notify.AA_MAY_APPEND,
			[]string{"access"},
		},
		{
			"camera",
			notify.AA_MAY_OPEN,
			[]string{"access"},
		},
	}
	for _, testCase := range cases {
		perms, err := common.AbstractPermissionsFromAppArmorPermissions(testCase.iface, testCase.mask)
		c.Check(err, IsNil, Commentf("testCase: %+v", testCase))
		c.Check(perms, DeepEquals, testCase.list)
	}
}

func (s *commonSuite) TestAbstractPermissionsFromAppArmorFilePermissionsUnhappy(c *C) {
	cases := []struct {
		iface  string
		perms  interface{}
		errStr string
	}{
		{
			"foo",
			"anything",
			".*unsupported interface.*",
		},
		{
			"home",
			"not a file permission",
			"failed to parse the given permissions as file permissions",
		},
		{
			"home",
			notify.FilePermission(1 << 17),
			"received unexpected permission for interface.*",
		},
		{
			"home",
			notify.AA_MAY_GETATTR | notify.AA_MAY_READ,
			"received unexpected permission for interface.*",
		},
		{
			"camera",
			notify.AA_MAY_EXEC,
			"received unexpected permission for interface.*",
		},
		{
			"camera",
			notify.AA_MAY_EXEC | notify.AA_MAY_READ,
			"received unexpected permission for interface.*",
		},
		{
			"home",
			notify.FilePermission(0),
			"no abstract permissions.*",
		},
	}
	for _, testCase := range cases {
		perms, err := common.AbstractPermissionsFromAppArmorPermissions(testCase.iface, testCase.perms)
		c.Check(perms, IsNil, Commentf("received unexpected non-nil permissions list for test case: %+v", testCase))
		c.Check(err, ErrorMatches, testCase.errStr)
	}
}

func (s *commonSuite) TestAbstractPermissionsFromListHappy(c *C) {
	cases := []struct {
		iface   string
		initial []string
		final   []string
	}{
		{
			"home",
			[]string{"write", "read", "execute"},
			[]string{"read", "write", "execute"},
		},
		{
			"home",
			[]string{"execute", "write", "read"},
			[]string{"read", "write", "execute"},
		},
		{
			"home",
			[]string{"write", "write", "write"},
			[]string{"write"},
		},
		{
			"camera",
			[]string{"access", "access", "access"},
			[]string{"access"},
		},
	}
	for _, testCase := range cases {
		perms, err := common.AbstractPermissionsFromList(testCase.iface, testCase.initial)
		c.Check(err, IsNil, Commentf("testCase: %+v", testCase))
		c.Check(perms, DeepEquals, testCase.final, Commentf("testCase: %+v", testCase))
	}
}

func (s *commonSuite) TestAbstractPermissionsFromListUnhappy(c *C) {
	cases := []struct {
		iface  string
		perms  []string
		errStr string
	}{
		{
			"foo",
			[]string{"read"},
			"unsupported interface.*",
		},
		{
			"home",
			[]string{"access"},
			"unsupported permission.*",
		},
		{
			"home",
			[]string{"read", "write", "access"},
			"unsupported permission.*",
		},
		{
			"camera",
			[]string{"read", "access"},
			"unsupported permission.*",
		},
		{
			"home",
			[]string{},
			fmt.Sprintf("%v", common.ErrPermissionsListEmpty),
		},
	}
	for _, testCase := range cases {
		perms, err := common.AbstractPermissionsFromList(testCase.iface, testCase.perms)
		c.Check(perms, IsNil, Commentf("testCase: %+v", testCase))
		c.Check(err, ErrorMatches, testCase.errStr, Commentf("testCase: %+v", testCase))
	}
}

func (s *commonSuite) TestAbstractPermissionsToAppArmorFilePermissionsHappy(c *C) {
	cases := []struct {
		iface string
		list  []string
		mask  notify.FilePermission
	}{
		{
			"home",
			[]string{"read"},
			notify.AA_MAY_OPEN | notify.AA_MAY_READ,
		},
		{
			"home",
			[]string{"write"},
			notify.AA_MAY_OPEN | notify.AA_MAY_WRITE | notify.AA_MAY_APPEND | notify.AA_MAY_CREATE | notify.AA_MAY_DELETE | notify.AA_MAY_RENAME | notify.AA_MAY_CHMOD | notify.AA_MAY_LOCK | notify.AA_MAY_LINK,
		},
		{
			"home",
			[]string{"execute"},
			notify.AA_MAY_OPEN | notify.AA_MAY_EXEC | notify.AA_EXEC_MMAP,
		},
		{
			"home",
			[]string{"read", "execute"},
			notify.AA_MAY_OPEN | notify.AA_MAY_READ | notify.AA_MAY_EXEC | notify.AA_EXEC_MMAP,
		},
		{
			"home",
			[]string{"execute", "write", "read"},
			notify.AA_MAY_OPEN | notify.AA_MAY_READ | notify.AA_MAY_EXEC | notify.AA_EXEC_MMAP | notify.AA_MAY_WRITE | notify.AA_MAY_APPEND | notify.AA_MAY_CREATE | notify.AA_MAY_DELETE | notify.AA_MAY_RENAME | notify.AA_MAY_CHMOD | notify.AA_MAY_LOCK | notify.AA_MAY_LINK,
		},
		{
			"camera",
			[]string{"access"},
			notify.AA_MAY_OPEN | notify.AA_MAY_WRITE | notify.AA_MAY_READ | notify.AA_MAY_APPEND,
		},
	}
	for _, testCase := range cases {
		ret, err := common.AbstractPermissionsToAppArmorPermissions(testCase.iface, testCase.list)
		c.Check(err, IsNil)
		perms, ok := ret.(notify.FilePermission)
		c.Check(ok, Equals, true, Commentf("failed to parse return value as FilePermission for test case: %+v", testCase))
		c.Check(perms, Equals, testCase.mask)
	}
}

func (s *commonSuite) TestAbstractPermissionsToAppArmorFilePermissionsUnhappy(c *C) {
	cases := []struct {
		iface  string
		perms  []string
		errStr string
	}{
		{
			"foo",
			[]string{},
			".*unsupported interface.*",
		},
		{
			"home",
			[]string{},
			fmt.Sprintf("%v", common.ErrPermissionsListEmpty),
		},
		{
			"home",
			[]string{"foo"},
			"no AppArmor file permission mapping .* abstract permission.*",
		},
		{
			"home",
			[]string{"access"},
			"no AppArmor file permission mapping .* abstract permission.*",
		},
		{
			"home",
			[]string{"read", "foo", "write"},
			"no AppArmor file permission mapping .* abstract permission.*",
		},
		{
			"camera",
			[]string{"read"},
			"no AppArmor file permission mapping .* abstract permission.*",
		},
	}
	for _, testCase := range cases {
		_, err := common.AbstractPermissionsToAppArmorPermissions(testCase.iface, testCase.perms)
		c.Check(err, ErrorMatches, testCase.errStr)
	}
}

func (s *commonSuite) TestValidatePathPattern(c *C) {
	for _, pattern := range []string{
		"/",
		"/*",
		"/**",
		"/**/*.txt",
		"/foo",
		"/foo",
		"/foo/file.txt",
		"/foo/bar",
		"/foo/bar/*",
		"/foo/bar/*.tar.gz",
		"/foo/bar/**",
		"/foo/bar/**/*.zip",
	} {
		c.Check(common.ValidatePathPattern(pattern), IsNil, Commentf("valid path pattern `%s` was incorrectly not allowed", pattern))
	}

	for _, pattern := range []string{
		"file.txt",
		"/**/*",
		"/foo/*/bar",
		"/foo/**/bar",
		"/foo/bar/",
		"/foo/bar*",
		"/foo/bar*.txt",
		"/foo/bar**",
		"/foo/bar/*txt",
		"/foo/bar/**.txt",
		"/foo/bar/*/file.txt",
		"/foo/bar/**/file.txt",
		"/foo/bar/**/*",
		"/foo/bar/**/*txt",
	} {
		c.Check(common.ValidatePathPattern(pattern), ErrorMatches, "invalid path pattern.*", Commentf("invalid path pattern %q was incorrectly allowed", pattern))
	}
}

func (s *commonSuite) TestValidateOutcome(c *C) {
	c.Assert(common.ValidateOutcome(common.OutcomeAllow), Equals, nil)
	c.Assert(common.ValidateOutcome(common.OutcomeDeny), Equals, nil)
	c.Assert(common.ValidateOutcome(common.OutcomeUnset), Equals, common.ErrInvalidOutcome)
	c.Assert(common.ValidateOutcome(common.OutcomeType("foo")), Equals, common.ErrInvalidOutcome)
}

func (s *commonSuite) TestValidateLifespanParseDuration(c *C) {
	unsetDuration := ""
	invalidDuration := "foo"
	negativeDuration := "-5s"
	validDuration := "10m"
	parsedValidDuration, err := time.ParseDuration(validDuration)
	c.Assert(err, IsNil)

	for _, lifespan := range []common.LifespanType{
		common.LifespanForever,
		common.LifespanSession,
		common.LifespanSingle,
	} {
		expiration, err := common.ValidateLifespanParseDuration(lifespan, unsetDuration)
		c.Check(expiration, Equals, "")
		c.Check(err, IsNil)
		for _, dur := range []string{invalidDuration, negativeDuration, validDuration} {
			expiration, err = common.ValidateLifespanParseDuration(lifespan, dur)
			c.Check(expiration, Equals, "")
			c.Check(err, Equals, common.ErrInvalidDurationForLifespan)
		}
	}

	expiration, err := common.ValidateLifespanParseDuration(common.LifespanTimespan, unsetDuration)
	c.Check(expiration, Equals, "")
	c.Check(err, Equals, common.ErrInvalidDurationEmpty)

	expiration, err = common.ValidateLifespanParseDuration(common.LifespanTimespan, invalidDuration)
	c.Check(expiration, Equals, "")
	c.Check(err, Equals, common.ErrInvalidDurationParseError)

	expiration, err = common.ValidateLifespanParseDuration(common.LifespanTimespan, negativeDuration)
	c.Check(expiration, Equals, "")
	c.Check(err, Equals, common.ErrInvalidDurationNegative)

	expiration, err = common.ValidateLifespanParseDuration(common.LifespanTimespan, validDuration)
	c.Check(err, Equals, nil)
	expirationTime, err := time.Parse(time.RFC3339, expiration)
	c.Check(err, IsNil)
	c.Check(expirationTime.After(time.Now()), Equals, true)
	c.Check(expirationTime.Before(time.Now().Add(parsedValidDuration)), Equals, true)
}

func (s *commonSuite) TestValidateConstraintsOutcomeLifespanDuration(c *C) {
	goodInterface := "home"
	badInterface := "foo"
	goodConstraints := &common.Constraints{
		PathPattern: "/path/to/something",
		Permissions: []string{"read"},
	}
	badConstraints := &common.Constraints{
		PathPattern: "bad\\path",
		Permissions: []string{"read"},
	}
	goodOutcome := common.OutcomeAllow
	badOutcome := common.OutcomeUnset
	goodLifespan := common.LifespanTimespan
	badLifespan := common.LifespanUnset
	goodDuration := "10s"
	badDuration := "foo"

	_, err := common.ValidateConstraintsOutcomeLifespanDuration(goodInterface, goodConstraints, goodOutcome, goodLifespan, goodDuration)
	c.Check(err, IsNil)
	_, err = common.ValidateConstraintsOutcomeLifespanDuration(badInterface, goodConstraints, goodOutcome, goodLifespan, goodDuration)
	c.Check(err, NotNil)
	_, err = common.ValidateConstraintsOutcomeLifespanDuration(goodInterface, badConstraints, goodOutcome, goodLifespan, goodDuration)
	c.Check(err, ErrorMatches, "invalid path pattern.*")
	_, err = common.ValidateConstraintsOutcomeLifespanDuration(goodInterface, goodConstraints, badOutcome, goodLifespan, goodDuration)
	c.Check(err, Equals, common.ErrInvalidOutcome)
	_, err = common.ValidateConstraintsOutcomeLifespanDuration(goodInterface, goodConstraints, goodOutcome, badLifespan, goodDuration)
	c.Check(err, Equals, common.ErrInvalidLifespan)
	_, err = common.ValidateConstraintsOutcomeLifespanDuration(goodInterface, goodConstraints, goodOutcome, goodLifespan, badDuration)
	c.Check(err, Equals, common.ErrInvalidDurationParseError)
}

func (s *commonSuite) TestGetHighestPrecedencePattern(c *C) {
	for i, testCase := range []struct {
		Patterns          []string
		HighestPrecedence string
	}{
		{
			[]string{
				"/foo",
			},
			"/foo",
		},
		{
			[]string{
				"/foo",
				"/foo/*",
			},
			"/foo",
		},
		{
			[]string{
				"/foo",
				"/foo/**",
			},
			"/foo",
		},
		{
			[]string{
				"/foo/*",
				"/foo/**",
			},
			"/foo/*",
		},
		{
			[]string{
				"/foo/**",
				"/foo/*",
			},
			"/foo/*",
		},
		{
			[]string{
				"/foo",
				"/foo/*",
				"/foo/**",
			},
			"/foo",
		},
		{
			[]string{
				"/foo/*",
				"/foo/bar",
			},
			"/foo/bar",
		},
		{
			[]string{
				"/foo/**",
				"/foo/bar",
			},
			"/foo/bar",
		},
		{
			[]string{
				"/foo/**",
				"/foo/bar/*",
			},
			"/foo/bar/*",
		},
		{
			[]string{
				"/foo/bar/**",
				"/foo/**",
			},
			"/foo/bar/**",
		},
		{
			[]string{
				"/foo/**",
				"/foo/bar/file.txt",
			},
			"/foo/bar/file.txt",
		},
		{
			[]string{
				"/foo/**/*.txt",
				"/foo/bar/**",
			},
			"/foo/**/*.txt",
		},
		{
			[]string{
				"/foo/**/*.gz",
				"/foo/**/*.tar.gz",
			},
			"/foo/**/*.tar.gz",
		},
		{
			[]string{
				"/foo/bar/**/*.gz",
				"/foo/**/*.tar.gz",
			},
			"/foo/**/*.tar.gz",
		},
	} {
		highestPrecedence, err := common.GetHighestPrecedencePattern(testCase.Patterns)
		c.Check(err, IsNil, Commentf("Error occurred during test case %d:\n%+v", i, testCase))
		c.Check(highestPrecedence, Equals, testCase.HighestPrecedence, Commentf("Highest precedence pattern incorrect for test case %d:\n%+v", i, testCase))
	}

	empty, err := common.GetHighestPrecedencePattern([]string{})
	c.Check(err, Equals, common.ErrNoPatterns)
	c.Check(empty, Equals, "")
}

func (*commonSuite) TestStripTrailingSlashes(c *C) {
	cases := []struct {
		orig     string
		stripped string
	}{
		{
			"foo",
			"foo",
		},
		{
			"foo/",
			"foo",
		},
		{
			"/foo",
			"/foo",
		},
		{
			"/foo/",
			"/foo",
		},
		{
			"/foo//",
			"/foo",
		},
		{
			"/foo///",
			"/foo",
		},
		{
			"/foo/bar",
			"/foo/bar",
		},
		{
			"/foo/bar/",
			"/foo/bar",
		},
		{
			"/foo/bar//",
			"/foo/bar",
		},
		{
			"/foo/bar///",
			"/foo/bar",
		},
	}

	for _, testCase := range cases {
		result := common.StripTrailingSlashes(testCase.orig)
		c.Check(result, Equals, testCase.stripped)
	}
}
