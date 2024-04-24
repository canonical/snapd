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
			"/home/test/Documents",
			"/home/test/Documents/",
			true,
		},
		{
			"/home/test/Documents/",
			"/home/test/Documents",
			false,
		},
		{
			"/home/test/Documents/",
			"/home/test/Documents/",
			true,
		},
		{
			"/home/test/Documents/*",
			"/home/test/Documents",
			false,
		},
		{
			"/home/test/Documents/*",
			"/home/test/Documents/",
			true,
		},
		{
			"/home/test/Documents/**",
			"/home/test/Documents",
			true,
		},
		{
			"/home/test/Documents/**",
			"/home/test/Documents/",
			true,
		},
		{
			"/home/test/Documents/**/",
			"/home/test/Documents",
			false,
		},
		{
			"/home/test/Documents/**/",
			"/home/test/Documents/",
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
		{
			"/foo/bar*",
			"/hoo/bar/",
			false,
		},
		{
			"/foo/bar/**",
			"/foo/bar/",
			true,
		},
		{
			"/foo/*/bar/**/baz**/fi*z/**buzz",
			"/foo/abc/bar/baznm/fizz/xyzbuzz",
			true,
		},
		{
			"/foo*bar",
			"/foobar",
			true,
		},
		{
			"/foo/*/bar",
			"/foo/bar",
			false,
		},
		{
			"/foo/**/bar",
			"/foo/bar",
			true,
		},
		{
			"/foo/**/bar",
			"/foo/bar/",
			true,
		},
		{
			"/foo/**/bar",
			"/foo/fizz/buzz/bar/",
			true,
		},
		{
			"/foo**/bar",
			"/fooabc/bar",
			true,
		},
		{
			"/foo**/bar",
			"/foo/bar",
			true,
		},
		{
			"/foo**/bar",
			"/foo/fizz/bar",
			false,
		},
		{
			"/foo/**bar",
			"/foo/abcbar",
			true,
		},
		{
			"/foo/**bar",
			"/foo/bar",
			true,
		},
		{
			"/foo/**bar",
			"/foo/fizz/bar",
			false,
		},
		{
			"/foo/*/bar/**/baz**/fi*z/**buzz",
			"/foo/abc/bar/baz/fiz/buzz",
			true,
		},
		{
			"/foo/*/bar/**/baz**/fi*z/**buzz",
			"/foo/abc/bar/baz/abc/fiz/buzz",
			false,
		},
		{
			"/foo/*/bar/**/baz**/fi*z/**buzz",
			"/foo/bar/bazmn/fizz/xyzbuzz",
			false,
		},
		{
			"/foo/bar/**/*",
			"/foo/bar",
			false,
		},
		{
			"/foo/bar/**/*",
			"/foo/bar/",
			false,
		},
		{
			"/foo/bar/**/*",
			"/foo/bar/baz",
			true,
		},
		{
			"/foo/bar/**/*/",
			"/foo/bar/baz",
			false,
		},
		{
			"/foo/bar/**/*",
			"/foo/bar/baz/",
			true,
		},
		{
			"/foo/bar/**/*/",
			"/foo/bar/baz/",
			true,
		},
		{
			"/foo/bar/**/*",
			"/foo/bar/baz/fizz",
			true,
		},
		{
			"/foo/bar/**/*/",
			"/foo/bar/baz/fizz",
			false,
		},
		{
			"/foo/bar/**/*.txt",
			"/foo/bar/baz.txt",
			true,
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
	badPath := `badpattern\`
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
	c.Check(err, ErrorMatches, `invalid outcome.*`)
	_, err = common.OutcomeType("foo").AsBool()
	c.Check(err, ErrorMatches, `invalid outcome.*`)
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
	c.Assert(parsedTimePaired.Equal(timestampPaired), Equals, true)
}

func (s *commonSuite) TestLabelToSnap(c *C) {
	cases := []struct {
		label string
		snap  string
	}{
		{
			label: "snap.nextcloud.occ",
			snap:  "nextcloud",
		},
		{
			label: "snap.lxd.lxc",
			snap:  "lxd",
		},
		{
			label: "snap.firefox.firefox",
			snap:  "firefox",
		},
	}
	for _, testCase := range cases {
		snap, err := common.LabelToSnap(testCase.label)
		c.Check(err, IsNil)
		c.Check(snap, Equals, testCase.snap)
	}
}

func (s *commonSuite) TestLabelToSnapUnhappy(c *C) {
	cases := []string{
		"snap",
		"snap.nextcloud",
		"nextcloud.occ",
		"snap.nextcloud.nextcloud.occ",
		"SNAP.NEXTCLOUD.OCC",
	}
	for _, label := range cases {
		snap, err := common.LabelToSnap(label)
		c.Check(err, Equals, common.ErrInvalidSnapLabel)
		c.Check(snap, Equals, label)
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

func (s *commonSuite) TestExpandPathPattern(c *C) {
	for _, testCase := range []struct {
		pattern  string
		expanded []string
	}{
		{
			`/foo`,
			[]string{`/foo`},
		},
		{
			`/{foo,bar/}`,
			[]string{`/foo`, `/bar/`},
		},
		{
			`{/foo,/bar/}`,
			[]string{`/foo`, `/bar/`},
		},
		{
			`/foo**/bar/*/**baz/**/fizz*buzz/**`,
			[]string{`/foo*/bar/*/*baz/**/fizz*buzz/**`},
		},
		{
			`/{,//foo**/bar/*/**baz/**/fizz*buzz/**}`,
			[]string{`/`, `/foo*/bar/*/*baz/**/fizz*buzz/**`},
		},
		{
			`/{foo,bar,/baz}`,
			[]string{`/foo`, `/bar`, `/baz`},
		},
		{
			`/{foo,/bar,bar,/baz}`,
			[]string{`/foo`, `/bar`, `/baz`},
		},
		{
			`/foo/bar\**baz`,
			[]string{`/foo/bar\**baz`},
		},
		{
			`/foo/bar/baz/**/*.txt`,
			[]string{`/foo/bar/baz/**/*.txt`},
		},
		{
			`/foo/bar/baz/***.txt`,
			[]string{`/foo/bar/baz/*.txt`},
		},
		{
			`/foo/bar/baz******.txt`,
			[]string{`/foo/bar/baz*.txt`},
		},
		{
			`/foo/bar/baz/{?***,*?**,**?*,***?}.txt`,
			[]string{`/foo/bar/baz/?*.txt`},
		},
		{
			`/foo/bar/baz/{?***?,*?**?,**?*?,***??}.txt`,
			[]string{`/foo/bar/baz/??*.txt`},
		},
		{
			`/foo/bar/baz/{?***??,*?**??,**?*??,***???}.txt`,
			[]string{`/foo/bar/baz/???*.txt`},
		},
		{
			`/foo///bar/**/**/**/baz/***.txt/**/**/*`,
			[]string{`/foo/bar/**/baz/*.txt/**`},
		},
		{
			`{a,b}c{d,e}f{g,h}`,
			[]string{
				`acdfg`,
				`acdfh`,
				`acefg`,
				`acefh`,
				`bcdfg`,
				`bcdfh`,
				`bcefg`,
				`bcefh`,
			},
		},
		{
			`a{{b,c},d,{e{f,{,g}}}}h`,
			[]string{
				`abh`,
				`ach`,
				`adh`,
				`aefh`,
				`aeh`,
				`aegh`,
			},
		},
		{
			`a{{b,c},d,\{e{f,{,g\}}}}h`,
			[]string{
				`abh`,
				`ach`,
				`adh`,
				`a\{efh`,
				`a\{eh`,
				`a\{eg\}h`,
			},
		},
	} {
		expanded, err := common.ExpandPathPattern(testCase.pattern)
		c.Check(err, IsNil, Commentf("test case: %+v", testCase))
		c.Check(expanded, DeepEquals, testCase.expanded, Commentf("test case: %+v", testCase))
	}
}

func (s *commonSuite) TestExpandPathPatternUnhappy(c *C) {
	for _, testCase := range []struct {
		pattern string
		errStr  string
	}{
		{
			``,
			`invalid path pattern: pattern has length 0`,
		},
		{
			`/foo{bar`,
			`invalid path pattern: unmatched '{' character.*`,
		},
		{
			`/foo}bar`,
			`invalid path pattern: unmatched '}' character.*`,
		},
		{
			`/foo/bar\`,
			`invalid path pattern: trailing unescaped '\\' character.*`,
		},
		{
			`/foo/bar{`,
			`invalid path pattern: unmatched '{' character.*`,
		},
		{
			`/foo/bar{baz\`,
			`invalid path pattern: trailing unescaped '\\' character.*`,
		},
		{
			`/foo/bar{baz{\`,
			`invalid path pattern: trailing unescaped '\\' character.*`,
		},
		{
			`/foo/bar{baz{`,
			`invalid path pattern: unmatched '{' character.*`,
		},
	} {
		result, err := common.ExpandPathPattern(testCase.pattern)
		c.Check(result, IsNil)
		c.Check(err, ErrorMatches, testCase.errStr)
	}
}

func (s *commonSuite) TestGetHighestPrecedencePattern(c *C) {
	for i, testCase := range []struct {
		patterns          []string
		highestPrecedence string
	}{
		{
			[]string{
				"/foo",
			},
			"/foo",
		},
		{
			[]string{
				"/foo/bar",
				"/foo",
				"/foo/bar/baz",
			},
			"/foo/bar/baz",
		},
		{
			[]string{
				"/foo",
				"/foo/barbaz",
				"/foobar",
			},
			"/foo/barbaz",
		},
		// Literals
		{
			[]string{
				"/foo/bar/baz",
				"/foo/bar/",
			},
			"/foo/bar/baz",
		},
		{
			[]string{
				"/foo/bar/baz",
				"/foo/bar/ba?",
			},
			"/foo/bar/baz",
		},
		{
			[]string{
				"/foo/bar/baz",
				"/foo/bar/b?z",
			},
			"/foo/bar/baz",
		},
		{
			[]string{
				"/foo/bar/",
				"/foo/bar",
			},
			"/foo/bar/",
		},
		{
			[]string{
				"/foo/bar",
				"/foo/ba?",
			},
			"/foo/bar",
		},
		{
			[]string{
				"/foo/bar/",
				"/foo/bar/*",
			},
			"/foo/bar/",
		},
		{
			[]string{
				"/foo/bar/",
				"/foo/bar/**",
			},
			"/foo/bar/",
		},
		{
			[]string{
				"/foo/bar/",
				"/foo/bar/**/",
			},
			"/foo/bar/",
		},
		// Terminated
		{
			[]string{
				"/foo/bar",
				"/foo/bar/**",
			},
			"/foo/bar",
		},
		{
			[]string{
				"/foo/bar",
				"/foo/bar*",
			},
			"/foo/bar",
		},
		// Any single character
		{
			[]string{
				"/foo/bar?baz",
				"/foo/bar*baz",
			},
			"/foo/bar?baz",
		},
		{
			[]string{
				"/foo/bar?baz",
				"/foo/bar**baz",
			},
			"/foo/bar?baz",
		},
		{
			[]string{
				"/foo/ba?",
				"/foo/ba*",
			},
			"/foo/ba?",
		},
		{
			[]string{
				"/foo/ba?/",
				"/foo/ba?",
			},
			"/foo/ba?/",
		},
		// Singlestars
		{
			[]string{
				"/foo/bar/*/baz",
				"/foo/bar/*/*baz",
			},
			"/foo/bar/*/baz",
		},
		{
			[]string{
				"/foo/bar/*/baz",
				"/foo/bar/*/*",
			},
			"/foo/bar/*/baz",
		},
		{
			[]string{
				"/foo/bar/*/",
				"/foo/bar/*/*",
			},
			"/foo/bar/*/",
		},
		{
			[]string{
				"/foo/bar/*/",
				"/foo/bar/*",
			},
			"/foo/bar/*/",
		},
		{
			[]string{
				"/foo/bar/*/",
				"/foo/bar/*/**/",
			},
			"/foo/bar/*/",
		},
		{
			[]string{
				"/foo/bar/*/",
				"/foo/bar/*/**",
			},
			"/foo/bar/*/",
		},
		{
			[]string{
				"/foo/bar/*/*baz",
				"/foo/bar/*/*",
			},
			"/foo/bar/*/*baz",
		},
		{
			[]string{
				"/foo/bar/*/*baz",
				"/foo/bar/*/**",
			},
			"/foo/bar/*/*baz",
		},
		{
			[]string{
				"/foo/bar/*/*",
				"/foo/bar/*/**",
			},
			"/foo/bar/*/*",
		},
		{
			[]string{
				"/foo/bar/*",
				"/foo/bar/*/**",
			},
			"/foo/bar/*",
		},
		{
			[]string{
				"/foo/bar/*",
				"/foo/bar/**/baz",
			},
			"/foo/bar/*",
		},
		{
			[]string{
				"/foo/bar/*/**",
				"/foo/bar/**/baz",
			},
			"/foo/bar/*/**",
		},
		// Globs
		{
			[]string{
				"/foo/bar*baz",
				"/foo/bar*",
			},
			"/foo/bar*baz",
		},
		{
			[]string{
				"/foo/bar*/baz",
				"/foo/bar*/",
			},
			"/foo/bar*/baz",
		},
		{
			[]string{
				"/foo/bar*/baz",
				"/foo/bar*/baz/**",
			},
			"/foo/bar*/baz",
		},
		{
			[]string{
				"/foo/bar*/baz",
				"/foo/bar/**/baz",
			},
			"/foo/bar*/baz",
		},
		{
			[]string{
				"/foo/bar*/baz",
				"/foo/bar/**/*baz/",
			},
			"/foo/bar*/baz",
		},
		{
			[]string{
				"/foo/bar*/baz",
				"/foo/bar/**",
			},
			"/foo/bar*/baz",
		},
		{
			[]string{
				"/foo/bar*/baz/**",
				"/foo/bar/**",
			},
			"/foo/bar*/baz/**",
		},
		{
			[]string{
				"/foo/bar*/",
				"/foo/bar*/*baz",
			},
			"/foo/bar*/",
		},
		{
			[]string{
				"/foo/bar*/",
				"/foo/bar*/*",
			},
			"/foo/bar*/",
		},
		{
			[]string{
				"/foo/bar*/",
				"/foo/bar*/**/",
			},
			"/foo/bar*/",
		},
		{
			[]string{
				"/foo/bar*/",
				"/foo/bar*/**",
			},
			"/foo/bar*/",
		},
		{
			[]string{
				"/foo/bar*/",
				"/foo/bar/**/",
			},
			"/foo/bar*/",
		},
		{
			[]string{
				"/foo/bar*/",
				"/foo/bar*/**/",
			},
			"/foo/bar*/",
		},
		{
			[]string{
				"/foo/bar*/*baz",
				"/foo/bar*/*",
			},
			"/foo/bar*/*baz",
		},
		{
			[]string{
				"/foo/bar*/*baz",
				"/foo/bar/**/baz",
			},
			"/foo/bar*/*baz",
		},
		{
			[]string{
				"/foo/bar*/*baz",
				"/foo/bar*/**/baz",
			},
			"/foo/bar*/*baz",
		},
		{
			[]string{
				"/foo/bar*/*/baz",
				"/foo/bar*/*/*",
			},
			"/foo/bar*/*/baz",
		},
		{
			[]string{
				"/foo/bar*/*/baz",
				"/foo/bar/**/baz",
			},
			"/foo/bar*/*/baz",
		},
		{
			[]string{
				"/foo/bar*/*/",
				"/foo/bar*/*",
			},
			"/foo/bar*/*/",
		},
		{
			[]string{
				"/foo/bar*/*/baz",
				"/foo/bar*/**/baz",
			},
			"/foo/bar*/*/baz",
		},
		{
			[]string{
				"/foo/bar*/*/",
				"/foo/bar/**/baz/",
			},
			"/foo/bar*/*/",
		},
		{
			[]string{
				"/foo/bar*/*/",
				"/foo/bar*/**/baz/",
			},
			"/foo/bar*/*/",
		},
		{
			[]string{
				"/foo/bar*/*",
				"/foo/bar/**/baz/",
			},
			"/foo/bar*/*",
		},
		{
			[]string{
				"/foo/bar*/*",
				"/foo/bar*/**/baz/",
			},
			"/foo/bar*/*",
		},
		{
			[]string{
				"/foo/bar*",
				"/foo/bar/**/",
			},
			"/foo/bar*",
		},
		{
			[]string{
				"/foo/bar*",
				"/foo/bar*/**/",
			},
			"/foo/bar*",
		},
		// Doublestars
		{
			[]string{
				"/foo/bar/**/baz",
				"/foo/bar/**/*baz",
			},
			"/foo/bar/**/baz",
		},
		{
			[]string{
				"/foo/bar/**/baz",
				"/foo/bar/**/*",
			},
			"/foo/bar/**/baz",
		},
		{
			[]string{
				"/foo/bar/**/*baz/",
				"/foo/bar/**/*baz",
			},
			"/foo/bar/**/*baz/",
		},
		{
			[]string{
				"/foo/bar/**/*baz/",
				"/foo/bar/**/",
			},
			"/foo/bar/**/*baz/",
		},
		{
			[]string{
				"/foo/bar/**/*baz",
				"/foo/bar/**/",
			},
			"/foo/bar/**/*baz",
		},
		{
			[]string{
				"/foo/bar/**/*baz",
				"/foo/bar/**/*",
			},
			"/foo/bar/**/*baz",
		},
		{
			[]string{
				"/foo/bar/**/*baz",
				"/foo/bar*/**/baz",
			},
			"/foo/bar/**/*baz",
		},
		{
			[]string{
				"/foo/bar/**/",
				"/foo/bar/**/*",
			},
			"/foo/bar/**/",
		},
		{
			[]string{
				"/foo/bar/**/",
				"/foo/bar/**",
			},
			"/foo/bar/**/",
		},
		{
			[]string{
				"/foo/bar/**/",
				"/foo/bar*/**/baz/",
			},
			"/foo/bar/**/",
		},
		{
			[]string{
				"/foo/bar/**/*",
				"/foo/bar*/**/baz/",
			},
			"/foo/bar/**/*",
		},
		{
			[]string{
				"/foo/bar/**",
				"/foo/bar*/**/baz/",
			},
			"/foo/bar/**",
		},
		// Globs followed by doublestars
		{
			[]string{
				"/foo/bar*/**/baz",
				"/foo/bar*/**/",
			},
			"/foo/bar*/**/baz",
		},
		{
			[]string{
				"/foo/bar*/**/",
				"/foo/bar*/**",
			},
			"/foo/bar*/**/",
		},
		// Miscellaneous
		{
			[]string{
				"/foo/bar/*.gz",
				"/foo/bar/*.tar.gz",
			},
			"/foo/bar/*.tar.gz",
		},
		{
			[]string{
				"/foo/bar/**/*.gz",
				"/foo/**/*.tar.gz",
			},
			"/foo/bar/**/*.gz",
		},
		{
			[]string{
				"/foo/bar/x/**/*.gz",
				"/foo/bar/**/*.tar.gz",
			},
			"/foo/bar/x/**/*.gz",
		},
		{
			// Both match `/foo/bar/baz.tar.gz`
			[]string{
				"/foo/bar/**/*.tar.gz",
				"/foo/bar/*",
			},
			"/foo/bar/*",
		},
		{
			[]string{
				"/foo/bar/**",
				"/foo/bar/baz/**",
				"/foo/bar/baz/**/*.txt",
			},
			"/foo/bar/baz/**/*.txt",
		},
		{
			// both match /foo/bar
			[]string{
				"/foo/bar*",
				"/foo/bar/**",
			},
			"/foo/bar*",
		},
		{
			[]string{
				"/foo/bar/*/baz*/**/fizz/*buzz",
				"/foo/bar/*/baz*/**/fizz/bu*zz",
				"/foo/bar/*/baz*/**/fizz/buzz",
				"/foo/bar/*/baz*/**/fizz/buzz*",
			},
			"/foo/bar/*/baz*/**/fizz/buzz",
		},
		{
			[]string{
				"/foo/*/bar/**",
				"/foo/**/bar/*",
			},
			"/foo/*/bar/**",
		},
		{
			[]string{
				`/foo/\\\b\a\r`,
				`/foo/barbaz`,
			},
			`/foo/barbaz`,
		},
		{
			[]string{
				`/foo/\\`,
				`/foo/*/bar/x`,
			},
			`/foo/\\`,
		},
		{
			[]string{
				`/foo/\**/b\ar/*\*`,
				`/foo/*/bar/x`,
			},
			`/foo/\**/b\ar/*\*`,
		},
		// Patterns with "**[^/]" should not be emitted from ExpandPathPattern
		{
			[]string{
				"/foo/**",
				"/foo/**bar",
			},
			"/foo/**bar",
		},
		// Duplicate patterns should never be passed into GetHighestPrecedencePattern
		{
			[]string{
				"/foo/bar/",
				"/foo/bar/",
				"/foo/bar",
			},
			"/foo/bar/",
		},
		{
			[]string{
				"/foo/bar/**",
				"/foo/bar/**",
				"/foo/bar/*",
			},
			"/foo/bar/*",
		},
	} {
		highestPrecedence, err := common.GetHighestPrecedencePattern(testCase.patterns)
		c.Check(err, IsNil, Commentf("Error occurred during test case %d:\n%+v", i, testCase))
		c.Check(highestPrecedence, Equals, testCase.highestPrecedence, Commentf("Highest precedence pattern incorrect for test case %d:\n%+v", i, testCase))
	}
}

func (s *commonSuite) TestGetHighestPrecedencePatternUnhappy(c *C) {
	empty, err := common.GetHighestPrecedencePattern([]string{})
	c.Check(err, Equals, common.ErrNoPatterns)
	c.Check(empty, Equals, "")

	result, err := common.GetHighestPrecedencePattern([]string{
		`/foo/bar`,
		`/foo/bar\`,
	})
	c.Check(err, ErrorMatches, "invalid path pattern.*")
	c.Check(result, Equals, "")
}

func (s *commonSuite) TestValidatePathPattern(c *C) {
	for _, pattern := range []string{
		"/",
		"/*",
		"/**",
		"/**/*.txt",
		"/foo",
		"/foo/",
		"/foo/file.txt",
		"/foo*",
		"/foo*bar",
		"/foo*bar/baz",
		"/foo/bar*baz",
		"/foo/*",
		"/foo/*bar",
		"/foo/*bar/",
		"/foo/*bar/baz",
		"/foo/*bar/baz/",
		"/foo/*/",
		"/foo/*/bar",
		"/foo/*/bar/",
		"/foo/*/bar/baz",
		"/foo/*/bar/baz/",
		"/foo/**/bar",
		"/foo/**/bar/",
		"/foo/**/bar/baz",
		"/foo/**/bar/baz/",
		"/foo/**/bar*",
		"/foo/**/bar*baz",
		"/foo/**/bar*baz/",
		"/foo/**/bar*/",
		"/foo/**/bar*/baz",
		"/foo/**/bar*/baz/fizz/",
		"/foo/**/bar/*",
		"/foo/**/bar/*.tar.gz",
		"/foo/**/bar/*baz",
		"/foo/**/bar/*baz/fizz/",
		"/foo/**/bar/*/",
		"/foo/**/bar/*baz",
		"/foo/**/bar/buzz/*baz/",
		"/foo/**/bar/*baz/fizz",
		"/foo/**/bar/buzz/*baz/fizz/",
		"/foo/**/bar/*/baz",
		"/foo/**/bar/buzz/*/baz/",
		"/foo/**/bar/*/baz/fizz",
		"/foo/**/bar/buzz/*/baz/fizz/",
		"/foo/**/bar/buzz*baz/fizz/",
		"/foo/**/*bar",
		"/foo/**/*bar/",
		"/foo/**/*bar/baz.tar.gz",
		"/foo/**/*bar/baz/",
		"/foo/**/*/",
		"/foo/**/*/bar",
		"/foo/**/*/bar/baz/",
		"/foo{,/,bar,*baz,*.baz,/*fizz,/*.fizz,/**/*buzz}",
		"/foo/{,*.bar,**/baz}",
		"/foo/bar/*",
		"/foo/bar/*.tar.gz",
		"/foo/bar/**",
		"/foo/bar/**/*.zip",
		"/foo/bar/**/*.tar.gz",
		`/foo/bar\,baz`,
		`/foo/bar\{baz`,
		`/foo/bar\\baz`,
		`/foo/bar\*baz`,
		`/foo/bar{,/baz/*,/fizz/**/*.txt}`,
		"/foo/*/bar",
		"/foo/bar/",
		"/foo/**/bar",
		"/foo/bar*",
		"/foo/bar*.txt",
		"/foo/bar/*txt",
		"/foo/bar/**/file.txt",
		"/foo/bar/*/file.txt",
		"/foo/bar/**/*txt",
		"/**/*",
		"/foo/bar**",
		"/foo/bar/**.txt",
		"/foo/bar/**/*",
		"/foo/ba,r",
		"/foo/ba,r/**/*.txt",
		"/foo/bar/**/*.txt,md",
		"/foo//bar",
		"/foo{//,bar}",
		"/foo{//*.bar,baz}",
		"/foo/{/*.bar,baz}",
		"/foo/*/**",
		"/foo/*/bar/**",
		"/foo/*/bar/*",
		"/foo{bar,/baz}{fizz,buzz}",
		"/foo{bar,/baz}/{fizz,buzz}",
		"/foo?bar",
	} {
		c.Check(common.ValidatePathPattern(pattern), IsNil, Commentf("valid path pattern `%s` was incorrectly not allowed", pattern))
	}

	for _, pattern := range []string{
		"file.txt",
		"/foo/bar{/**/*.txt",
		"/foo/bar/**/*.{txt",
		"{,/foo}",
		"{/,foo}",
		"/foo/ba[rz]",
		`/foo/bar\`,
	} {
		c.Check(common.ValidatePathPattern(pattern), ErrorMatches, "invalid path pattern.*", Commentf("invalid path pattern %q was incorrectly allowed", pattern))
	}
}

func (s *commonSuite) TestValidateOutcome(c *C) {
	c.Assert(common.ValidateOutcome(common.OutcomeAllow), Equals, nil)
	c.Assert(common.ValidateOutcome(common.OutcomeDeny), Equals, nil)
	c.Assert(common.ValidateOutcome(common.OutcomeUnset), ErrorMatches, `invalid outcome.*`)
	c.Assert(common.ValidateOutcome(common.OutcomeType("foo")), ErrorMatches, `invalid outcome.*`)
}

func (s *commonSuite) TestValidateLifespanExpiration(c *C) {
	var unsetExpiration *time.Time
	currTime := time.Now()
	negativeExpirationValue := currTime.Add(-5 * time.Second)
	negativeExpiration := &negativeExpirationValue
	validExpirationValue := currTime.Add(10 * time.Minute)
	validExpiration := &validExpirationValue

	for _, lifespan := range []common.LifespanType{
		common.LifespanForever,
		common.LifespanSession,
		common.LifespanSingle,
	} {
		err := common.ValidateLifespanExpiration(lifespan, unsetExpiration, currTime)
		c.Check(err, IsNil)
		for _, exp := range []*time.Time{negativeExpiration, validExpiration} {
			err = common.ValidateLifespanExpiration(lifespan, exp, currTime)
			c.Check(err, ErrorMatches, `invalid expiration: expiration must be empty.*`)
		}
	}

	err := common.ValidateLifespanExpiration(common.LifespanTimespan, unsetExpiration, currTime)
	c.Check(err, ErrorMatches, `invalid expiration: expiration must be non-empty.*`)

	err = common.ValidateLifespanExpiration(common.LifespanTimespan, negativeExpiration, currTime)
	c.Check(err, ErrorMatches, `invalid expiration: expiration time has already passed.*`)

	err = common.ValidateLifespanExpiration(common.LifespanTimespan, validExpiration, currTime)
	c.Check(err, IsNil)
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
		c.Check(expiration, IsNil)
		c.Check(err, IsNil)
		for _, dur := range []string{invalidDuration, negativeDuration, validDuration} {
			expiration, err = common.ValidateLifespanParseDuration(lifespan, dur)
			c.Check(expiration, IsNil)
			c.Check(err, ErrorMatches, `invalid duration: duration must be empty.*`)
		}
	}

	expiration, err := common.ValidateLifespanParseDuration(common.LifespanTimespan, unsetDuration)
	c.Check(expiration, IsNil)
	c.Check(err, ErrorMatches, `invalid duration: duration must be non-empty.*`)

	expiration, err = common.ValidateLifespanParseDuration(common.LifespanTimespan, invalidDuration)
	c.Check(expiration, IsNil)
	c.Check(err, ErrorMatches, `invalid duration: error parsing duration string.*`)

	expiration, err = common.ValidateLifespanParseDuration(common.LifespanTimespan, negativeDuration)
	c.Check(expiration, IsNil)
	c.Check(err, ErrorMatches, `invalid duration: duration must be greater than zero.*`)

	expiration, err = common.ValidateLifespanParseDuration(common.LifespanTimespan, validDuration)
	c.Check(err, IsNil)
	c.Check(expiration.After(time.Now()), Equals, true)
	c.Check(expiration.Before(time.Now().Add(parsedValidDuration)), Equals, true)
}

func (s *commonSuite) TestValidateConstraintsOutcomeLifespanExpiration(c *C) {
	goodInterface := "home"
	badInterface := "foo"
	goodConstraints := &common.Constraints{
		PathPattern: "/path/to/something",
		Permissions: []string{"read", "write", "execute"},
	}
	badConstraints := &common.Constraints{
		PathPattern: "/path{with*,groups?}/**",
		Permissions: []string{"read", "write", "append"},
	}
	goodOutcome := common.OutcomeDeny
	badOutcome := common.OutcomeUnset
	goodLifespan := common.LifespanTimespan
	badLifespan := common.LifespanType("foo")
	currTime := time.Now()
	goodExpiration := currTime.Add(10 * time.Second)
	badExpiration := currTime.Add(-1 * time.Second)

	err := common.ValidateConstraintsOutcomeLifespanExpiration(goodInterface, goodConstraints, goodOutcome, goodLifespan, &goodExpiration, currTime)
	c.Check(err, IsNil)
	err = common.ValidateConstraintsOutcomeLifespanExpiration(badInterface, goodConstraints, goodOutcome, goodLifespan, &goodExpiration, currTime)
	c.Check(err, NotNil)
	err = common.ValidateConstraintsOutcomeLifespanExpiration(goodInterface, badConstraints, goodOutcome, goodLifespan, &goodExpiration, currTime)
	c.Check(err, ErrorMatches, "unsupported permission.*")
	err = common.ValidateConstraintsOutcomeLifespanExpiration(goodInterface, goodConstraints, badOutcome, goodLifespan, &goodExpiration, currTime)
	c.Check(err, ErrorMatches, "invalid outcome.*")
	err = common.ValidateConstraintsOutcomeLifespanExpiration(goodInterface, goodConstraints, goodOutcome, badLifespan, &goodExpiration, currTime)
	c.Check(err, ErrorMatches, "invalid lifespan.*")
	err = common.ValidateConstraintsOutcomeLifespanExpiration(goodInterface, goodConstraints, goodOutcome, goodLifespan, &badExpiration, currTime)
	c.Check(err, ErrorMatches, "invalid expiration.*")
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
	c.Check(err, ErrorMatches, "invalid outcome.*")
	_, err = common.ValidateConstraintsOutcomeLifespanDuration(goodInterface, goodConstraints, goodOutcome, badLifespan, goodDuration)
	c.Check(err, ErrorMatches, "invalid lifespan.*")
	_, err = common.ValidateConstraintsOutcomeLifespanDuration(goodInterface, goodConstraints, goodOutcome, goodLifespan, badDuration)
	c.Check(err, ErrorMatches, "invalid duration.*")
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
