package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/testutil"
	. "gopkg.in/check.v1"
)

type hybridUserImportSuite struct {
	testutil.BaseTest
}

var _ = Suite(&hybridUserImportSuite{})

func (s *hybridUserImportSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

func assertFileHasPermissions(c *C, path string, perms os.FileMode) {
	fi, err := os.Stat(path)
	c.Assert(err, IsNil)
	c.Check(fi.Mode(), Equals, perms)
}

func assertLinesInFile(c *C, path string, lines []string) {
	contents, err := os.ReadFile(path)
	c.Assert(err, IsNil)

	split := strings.Split(string(contents), "\n")

	// +1 for the trailing newline
	c.Assert(split, HasLen, len(lines)+1)

	c.Check(split[:len(lines)], testutil.DeepUnsortedMatches, lines)
	c.Check(split[len(lines)], Equals, "")
}

func writeTempFile(c *C, path string, content string) string {
	err := os.MkdirAll(filepath.Dir(path), 0755)
	c.Assert(err, IsNil)

	f, err := os.Create(path)
	c.Assert(err, IsNil)
	defer f.Close()

	_, err = f.WriteString(content)
	c.Assert(err, IsNil)

	return f.Name()
}

func (s *hybridUserImportSuite) TestEntriesByName(c *C) {
	path := writeTempFile(c, filepath.Join(c.MkDir(), "passwd"), `
root:x:0:0:root:/root:/bin/bash
user1:x:1:1:user1:/home/user1:/bin/bash
user2:x:2:2:user2:/home/user2:/bin/bash
user3:x:3:3:user3:/home/user3:/bin/bash

invalid-line
`)

	entries, err := entriesByName(path)
	c.Assert(err, IsNil)

	c.Assert(entries, DeepEquals, map[string]string{
		"root":  "root:x:0:0:root:/root:/bin/bash",
		"user1": "user1:x:1:1:user1:/home/user1:/bin/bash",
		"user2": "user2:x:2:2:user2:/home/user2:/bin/bash",
		"user3": "user3:x:3:3:user3:/home/user3:/bin/bash",
	})
}

func (s *hybridUserImportSuite) TestParseUsersForGroup(c *C) {
	path := writeTempFile(c, filepath.Join(c.MkDir(), "group"), `
user1:x:1000:
user2:x:1001:
wheel:x:998:user1,user2
docker:x:968:user2
lxd:x:964:user1
sudo:x:959:user1,user2

invalid-line:
invalid-line
`)

	usersToGroups, err := parseGroupsForUser(path)
	c.Assert(err, IsNil)

	c.Assert(usersToGroups, DeepEquals, map[string][]string{
		"user1": {"wheel", "lxd", "sudo"},
		"user2": {"wheel", "docker", "sudo"},
	})
}

func (s *hybridUserImportSuite) TestParseGroups(c *C) {
	dir := c.MkDir()
	writeTempFile(c, filepath.Join(dir, "etc/group"), `
user1:x:1000:
user2:x:1001:
wheel:x:998:user1,user2
docker:x:968:user2
lxd:x:964:user1
sudo:x:959:user1,user2
user0:x:invalid-gid:

invalid-line:
invalid-line
`)

	writeTempFile(c, filepath.Join(dir, "etc/gshadow"), `
user1:!::
user2:!::
wheel:!::user1,user2
docker:!::user2
lxd:!::user1
sudo:!::user1,user2

invalid-line:
invalid-line
`)

	groups, err := parseGroups(dir, func(group) bool {
		return true
	})
	c.Assert(err, IsNil)

	c.Assert(groups, DeepEquals, map[string]group{
		"user1": {
			name:         "user1",
			gid:          1000,
			users:        nil,
			groupEntry:   "user1:x:1000:",
			gshadowEntry: "user1:!::",
		},
		"user2": {
			name:         "user2",
			gid:          1001,
			users:        nil,
			groupEntry:   "user2:x:1001:",
			gshadowEntry: "user2:!::",
		},
		"wheel": {
			name:         "wheel",
			gid:          998,
			users:        []string{"user1", "user2"},
			groupEntry:   "wheel:x:998:user1,user2",
			gshadowEntry: "wheel:!::user1,user2",
		},
		"docker": {
			name:         "docker",
			gid:          968,
			users:        []string{"user2"},
			groupEntry:   "docker:x:968:user2",
			gshadowEntry: "docker:!::user2",
		},
		"lxd": {
			name:         "lxd",
			gid:          964,
			users:        []string{"user1"},
			groupEntry:   "lxd:x:964:user1",
			gshadowEntry: "lxd:!::user1",
		},
		"sudo": {
			name:         "sudo",
			gid:          959,
			users:        []string{"user1", "user2"},
			groupEntry:   "sudo:x:959:user1,user2",
			gshadowEntry: "sudo:!::user1,user2",
		},
	})

	groups, err = parseGroups(dir, func(g group) bool {
		return g.name == "sudo"
	})
	c.Assert(err, IsNil)

	c.Assert(groups, DeepEquals, map[string]group{
		"sudo": {
			name:         "sudo",
			gid:          959,
			users:        []string{"user1", "user2"},
			groupEntry:   "sudo:x:959:user1,user2",
			gshadowEntry: "sudo:!::user1,user2",
		},
	})
}

func (s *hybridUserImportSuite) TestParseUsers(c *C) {
	dir := c.MkDir()
	writeTempFile(c, filepath.Join(dir, "etc/passwd"), `
root:x:0:0:root:/root:/bin/bash
user1:x:1000:1000:user1:/home/user1:/bin/bash
user2:x:1001:1001:user2:/home/user2:/usr/bin/fish
user3:x:1002:1002:user3:/home/user3:/usr/bin/zsh

invalid-line:
invalid-line
user4:x:invalid-uid:1003:user4:/home/user4:/usr/bin/zsh
user5:x:1004:invalid-gid:user5:/home/user5:/usr/bin/zsh
`)

	writeTempFile(c, filepath.Join(dir, "etc/group"), `
user1:x:1000:
user2:x:1001:
wheel:x:998:user1,user2
docker:x:968:user2
lxd:x:964:user1,user3
sudo:x:959:user1,user2

invalid-line:
invalid-line
`)

	writeTempFile(c, filepath.Join(dir, "etc/shadow"), `
root:d41d8cd98f00b204e9800998ecf8427e:19836:0:99999:7:::
user1:28a4517315bfa7bda8fdcfb2f1f1d042:19837:0:99999:7:::
user2:5177bcdd67b77a393852bb5ae47ee416:19838:0:99999:7:::
user3:028deb5e54d669e73be35cb5a96eb35b:19839:0:99999:7:::

invalid-line:
invalid-line
`)

	users, err := parseUsers(dir, func(user) bool {
		return true
	})
	c.Assert(err, IsNil)

	c.Assert(users, DeepEquals, map[string]user{
		"root": {
			name:        "root",
			uid:         0,
			gid:         0,
			groups:      nil,
			passwdEntry: "root:x:0:0:root:/root:/bin/bash",
			shadowEntry: "root:d41d8cd98f00b204e9800998ecf8427e:19836:0:99999:7:::",
		},
		"user1": {
			name:        "user1",
			uid:         1000,
			gid:         1000,
			groups:      []string{"wheel", "lxd", "sudo"},
			passwdEntry: "user1:x:1000:1000:user1:/home/user1:/bin/bash",
			shadowEntry: "user1:28a4517315bfa7bda8fdcfb2f1f1d042:19837:0:99999:7:::",
		},
		"user2": {
			name:        "user2",
			uid:         1001,
			gid:         1001,
			groups:      []string{"wheel", "docker", "sudo"},
			passwdEntry: "user2:x:1001:1001:user2:/home/user2:/usr/bin/fish",
			shadowEntry: "user2:5177bcdd67b77a393852bb5ae47ee416:19838:0:99999:7:::",
		},
		"user3": {
			name:        "user3",
			uid:         1002,
			gid:         1002,
			groups:      []string{"lxd"},
			passwdEntry: "user3:x:1002:1002:user3:/home/user3:/usr/bin/zsh",
			shadowEntry: "user3:028deb5e54d669e73be35cb5a96eb35b:19839:0:99999:7:::",
		},
	})

	users, err = parseUsers(dir, func(u user) bool {
		return u.name == "user1"
	})
	c.Assert(err, IsNil)

	c.Assert(users, DeepEquals, map[string]user{
		"user1": {
			name:        "user1",
			uid:         1000,
			gid:         1000,
			groups:      []string{"wheel", "lxd", "sudo"},
			passwdEntry: "user1:x:1000:1000:user1:/home/user1:/bin/bash",
			shadowEntry: "user1:28a4517315bfa7bda8fdcfb2f1f1d042:19837:0:99999:7:::",
		},
	})
}

func (s *hybridUserImportSuite) TestFilterUsers(c *C) {
	users := map[string]user{
		"root": {
			name: "root",
			uid:  0,
			gid:  0,
		},
		"user1": {
			name:   "user1",
			uid:    1000,
			gid:    1000,
			groups: []string{"wheel", "lxd", "sudo"},
		},
		"user2": {
			name:   "user2",
			uid:    1001,
			gid:    1001,
			groups: []string{"wheel", "docker", "admin"},
		},
		"user3": {
			name:   "user3",
			uid:    1002,
			gid:    1002,
			groups: []string{"lxd"},
		},
		"system-gid": {
			name: "system-gid",
			uid:  1002,
			gid:  998,
		},
		"lxd": {
			name: "lxd",
			uid:  999,
			gid:  999,
		},
	}

	targets := []string{"admin", "sudo"}
	filter := userFilter(targets)
	c.Assert(filter(users["root"]), Equals, true)
	c.Assert(filter(users["user1"]), Equals, true)
	c.Assert(filter(users["user2"]), Equals, true)
	c.Assert(filter(users["user3"]), Equals, false)
	c.Assert(filter(users["system-gid"]), Equals, false)
	c.Assert(filter(users["lxd"]), Equals, false)
}

func (s *hybridUserImportSuite) TestFilterGroups(c *C) {
	groups := map[string]group{
		"root": {
			name: "root",
			gid:  0,
		},
		"user1": {
			name: "user1",
			gid:  1000,
		},
		"user2": {
			name: "user2",
			gid:  1001,
		},
		"user3": {
			name: "user3",
			gid:  1002,
		},
		"docker": {
			name: "docker",
			gid:  999,
		},
	}

	users := map[string]user{
		"root": {
			name: "root",
			uid:  0,
			gid:  0,
		},
		"user1": {
			name:   "user1",
			uid:    1000,
			gid:    1000,
			groups: []string{"wheel", "lxd", "sudo"},
		},
		"user2": {
			name:   "user2",
			uid:    1001,
			gid:    1001,
			groups: []string{"wheel", "docker", "admin"},
		},
	}
	filter := groupFilter(users)

	c.Assert(filter(groups["root"]), Equals, true)
	c.Assert(filter(groups["user1"]), Equals, true)
	c.Assert(filter(groups["user2"]), Equals, true)
	c.Assert(filter(groups["user3"]), Equals, false)
	c.Assert(filter(groups["docker"]), Equals, false)
}

func (s *hybridUserImportSuite) TestMergeAndWriteGroupFiles(c *C) {
	dir := c.MkDir()
	writeTempFile(c, filepath.Join(dir, "etc/group"), `
root:x:0:
ignored:x:1000:
wheel:x:998:user1
docker:x:968:
lxd:x:964:
sudo:x:959:
user1:x:999:
`)

	writeTempFile(c, filepath.Join(dir, "etc/gshadow"), `
root:::
ignored:!::
wheel:!::user1
docker:!::
sudo:!::
user1:!::
`)

	dest := filepath.Join(dir, "etc-merged")
	err := os.MkdirAll(dest, 0755)
	c.Assert(err, IsNil)

	users := map[string]user{
		"root": {
			name:        "root",
			uid:         0,
			gid:         0,
			passwdEntry: "root:x:0:0:root:/root:",
			shadowEntry: "root:d41d8cd98f00b204e9800998ecf8427e:19836:0:99999:7:::",
		},
		"user1": {
			name:        "user1",
			uid:         1000,
			gid:         1000,
			groups:      []string{"wheel", "lxd", "sudo"},
			passwdEntry: "user1:x:1000:1000:user1:/home/user1:",
			shadowEntry: "user1:28a4517315bfa7bda8fdcfb2f1f1d042:19837:0:99999:7:::",
		},
		"user2": {
			name:        "user2",
			uid:         1001,
			gid:         1001,
			groups:      []string{"wheel", "docker", "sudo"},
			passwdEntry: "user2:x:1001:1001:user2:/home/user2:",
			shadowEntry: "user2:5177bcdd67b77a393852bb5ae47ee416:19838:0:99999:7:::",
		},
	}

	groups := map[string]group{
		"root": {
			name:         "root",
			gid:          0,
			groupEntry:   "root:x:0:",
			gshadowEntry: "root:!::",
		},
		"user1": {
			name:         "user1",
			gid:          1000,
			groupEntry:   "user1:x:1000:",
			gshadowEntry: "user1:!::",
		},
		"user2": {
			name:         "user2",
			gid:          1001,
			groupEntry:   "user2:x:1001:",
			gshadowEntry: "user2:!::",
		},
	}

	err = mergeAndWriteGroupFiles(dir, dest, users, groups)
	c.Assert(err, IsNil)

	assertLinesInFile(c, filepath.Join(dest, "group"), []string{
		"root:x:0:",
		"wheel:x:998:user1,user2",
		"docker:x:968:user2",
		"lxd:x:964:user1",
		"sudo:x:959:user1,user2",
		"user1:x:1000:",
		"user2:x:1001:",
	})
	assertFileHasPermissions(c, filepath.Join(dest, "group"), 0644)

	assertLinesInFile(c, filepath.Join(dest, "gshadow"), []string{
		"root:!::",
		"wheel:!::user1,user2",
		"docker:!::user2",
		"sudo:!::user1,user2",
		"user1:!::",
		"user2:!::",
	})
	assertFileHasPermissions(c, filepath.Join(dest, "gshadow"), 0600)
}

func (s *hybridUserImportSuite) TestMergeAndWriteGroupFilesSharedPrimary(c *C) {
	dir := c.MkDir()
	writeTempFile(c, filepath.Join(dir, "etc/group"), `
root:x:0:
wheel:x:998:
docker:x:968:
lxd:x:964:
sudo:x:959:
`)

	writeTempFile(c, filepath.Join(dir, "etc/gshadow"), `
root:::
wheel:!::
docker:!::
sudo:!::
`)

	dest := filepath.Join(dir, "etc-merged")
	err := os.MkdirAll(dest, 0755)
	c.Assert(err, IsNil)

	users := map[string]user{
		"root": {
			name:        "root",
			uid:         0,
			gid:         0,
			passwdEntry: "root:x:0:0:root:/root:",
			shadowEntry: "root:d41d8cd98f00b204e9800998ecf8427e:19836:0:99999:7:::",
		},
		"user1": {
			name:        "user1",
			uid:         1001,
			gid:         1000,
			groups:      []string{"wheel", "lxd", "sudo"},
			passwdEntry: "user1:x:1001:1000:user1:/home/user1:",
			shadowEntry: "user1:28a4517315bfa7bda8fdcfb2f1f1d042:19837:0:99999:7:::",
		},
		"user2": {
			name:        "user2",
			uid:         1002,
			gid:         1000,
			groups:      []string{"wheel", "docker", "sudo"},
			passwdEntry: "user2:x:1002:1000:user2:/home/user2:",
			shadowEntry: "user2:5177bcdd67b77a393852bb5ae47ee416:19838:0:99999:7:::",
		},
	}

	groups := map[string]group{
		"root": {
			name:         "root",
			gid:          0,
			groupEntry:   "root:x:0:",
			gshadowEntry: "root:!::",
		},
		"primary": {
			name:         "primary",
			gid:          1000,
			groupEntry:   "primary:x:1000:",
			gshadowEntry: "primary:!::",
		},
	}

	err = mergeAndWriteGroupFiles(dir, dest, users, groups)
	c.Assert(err, IsNil)

	assertLinesInFile(c, filepath.Join(dest, "group"), []string{
		"root:x:0:",
		"wheel:x:998:user1,user2",
		"docker:x:968:user2",
		"lxd:x:964:user1",
		"sudo:x:959:user1,user2",
		"primary:x:1000:",
	})
	assertFileHasPermissions(c, filepath.Join(dest, "group"), 0644)

	assertLinesInFile(c, filepath.Join(dest, "gshadow"), []string{
		"root:!::",
		"wheel:!::user1,user2",
		"docker:!::user2",
		"sudo:!::user1,user2",
		"primary:!::",
	})
	assertFileHasPermissions(c, filepath.Join(dest, "gshadow"), 0600)
}

func (s *hybridUserImportSuite) TestMergeAndWriteGroupFilesFailToImportSystemGroup(c *C) {
	dir := c.MkDir()
	writeTempFile(c, filepath.Join(dir, "etc/group"), `
root:x:0:
sudo:x:959:
`)

	writeTempFile(c, filepath.Join(dir, "etc/gshadow"), `
root:::
sudo:!::
`)

	dest := filepath.Join(dir, "etc-merged")
	err := os.MkdirAll(dest, 0755)
	c.Assert(err, IsNil)

	users := map[string]user{
		"root": {
			name:        "root",
			uid:         0,
			gid:         0,
			passwdEntry: "root:x:0:0:root:/root:",
			shadowEntry: "root:d41d8cd98f00b204e9800998ecf8427e:19836:0:99999:7:::",
		},
		"user1": {
			name:        "user1",
			uid:         1000,
			gid:         999,
			groups:      []string{"sudo"},
			passwdEntry: "user1:x:1000:999:user1:/home/user1:",
			shadowEntry: "user1:28a4517315bfa7bda8fdcfb2f1f1d042:19837:0:99999:7:::",
		},
	}

	groups := map[string]group{
		"root": {
			name:         "root",
			gid:          0,
			groupEntry:   "root:x:0:",
			gshadowEntry: "root:!::",
		},
		"user1": {
			name:         "user1",
			gid:          999,
			groupEntry:   "user1:x:999:",
			gshadowEntry: "user1:!::",
		},
	}

	err = mergeAndWriteGroupFiles(dir, dest, users, groups)
	c.Assert(err, ErrorMatches, "internal error: cannot import system groups: user1 has gid 999")
}

func (s *hybridUserImportSuite) TestMergeAndWriteUserFiles(c *C) {
	dir := c.MkDir()
	writeTempFile(c, filepath.Join(dir, "etc/passwd"), `
root:x:0:0:root:/root:/bin/bash
user1:x:1010:1010:user1:/home/user1:/bin/bash
lxd:x:964:984::/var/snap/lxd/common/lxd:/bin/false
user3:x:1011:1011:user3:/home/user3:/bin/bash
`)

	writeTempFile(c, filepath.Join(dir, "etc/group"), `
root:x:0:
wheel:x:998:
docker:x:968:
lxd:x:964:
sudo:x:959:
`)

	writeTempFile(c, filepath.Join(dir, "etc/shadow"), `
root:!:19836:0:99999:7:::
user1:28n4517315osn7oqn8sqpso2s1s1q042:19837:0:99999:7:::
lxd:!:19838:0:99999:7:::
user3:dc9b7b7b631aadd960231f4880923d0f:19839:0:99999:7:::
`)

	dest := filepath.Join(dir, "etc-merged")
	err := os.MkdirAll(dest, 0755)
	c.Assert(err, IsNil)

	// root will replace the original root, user1 will also replace the original
	// user1. user2 will be directly imported. user3 will be dropped despite
	// being in the original file, since we don't want to import non-system
	// users.
	users := map[string]user{
		"root": {
			name:        "root",
			uid:         0,
			gid:         0,
			groups:      nil,
			passwdEntry: "root:x:0:0:root:/root:/bin/bash",
			shadowEntry: "root:d41d8cd98f00b204e9800998ecf8427e:19836:0:99999:7:::",
		},
		"user1": {
			name:        "user1",
			uid:         1000,
			gid:         1000,
			groups:      []string{"wheel", "lxd", "sudo"},
			passwdEntry: "user1:x:1000:1000:user1:/home/user1:/bin/bash",
			shadowEntry: "user1:28a4517315bfa7bda8fdcfb2f1f1d042:19837:0:99999:7:::",
		},
		"user2": {
			name:        "user2",
			uid:         1001,
			gid:         1001,
			groups:      []string{"wheel", "docker", "sudo"},
			passwdEntry: "user2:x:1001:1001:user2:/home/user2:/usr/bin/fish",
			shadowEntry: "user2:5177bcdd67b77a393852bb5ae47ee416:19838:0:99999:7:::",
		},
	}

	err = mergeAndWriteUserFiles(dir, dest, users)
	c.Assert(err, IsNil)

	// note that users lost their shells. we're not importing them since they
	// might not be installed on the system that we're importing into.
	assertLinesInFile(c, filepath.Join(dest, "passwd"), []string{
		"root:x:0:0:root:/root:",
		"lxd:x:964:984::/var/snap/lxd/common/lxd:/bin/false",
		"user1:x:1000:1000:user1:/home/user1:",
		"user2:x:1001:1001:user2:/home/user2:",
	})
	assertFileHasPermissions(c, filepath.Join(dest, "passwd"), 0644)

	assertLinesInFile(c, filepath.Join(dest, "shadow"), []string{
		"root:d41d8cd98f00b204e9800998ecf8427e:19836:0:99999:7:::",
		"lxd:!:19838:0:99999:7:::",
		"user1:28a4517315bfa7bda8fdcfb2f1f1d042:19837:0:99999:7:::",
		"user2:5177bcdd67b77a393852bb5ae47ee416:19838:0:99999:7:::",
	})
	assertFileHasPermissions(c, filepath.Join(dest, "shadow"), 0600)
}

func (s *hybridUserImportSuite) TestMergeAndWriteUserFilesFailToImportSystemUser(c *C) {
	dir := c.MkDir()
	writeTempFile(c, filepath.Join(dir, "etc/passwd"), `
root:x:0:0:root:/root:/bin/bash
lxd:x:964:984::/var/snap/lxd/common/lxd:/bin/false
`)

	writeTempFile(c, filepath.Join(dir, "etc/group"), `
root:x:0:
wheel:x:998:
docker:x:968:
lxd:x:964:
sudo:x:959:
`)

	writeTempFile(c, filepath.Join(dir, "etc/shadow"), `
root:!:19836:0:99999:7:::
lxd:!:19838:0:99999:7:::
`)

	dest := filepath.Join(dir, "etc-merged")
	err := os.MkdirAll(dest, 0755)
	c.Assert(err, IsNil)

	users := map[string]user{
		"root": {
			name:        "root",
			uid:         0,
			gid:         0,
			groups:      nil,
			passwdEntry: "root:x:0:0:root:/root:/bin/bash",
			shadowEntry: "root:d41d8cd98f00b204e9800998ecf8427e:19836:0:99999:7:::",
		},
		"user1": {
			name:        "user1",
			uid:         999,
			gid:         999,
			groups:      []string{"wheel", "lxd", "sudo"},
			passwdEntry: "user1:x:999:999:user1:/home/user1:/bin/bash",
			shadowEntry: "user1:28a4517315bfa7bda8fdcfb2f1f1d042:19837:0:99999:7:::",
		},
	}

	err = mergeAndWriteUserFiles(dir, dest, users)
	c.Assert(err, ErrorMatches, "internal error: cannot import system users: user1 has uid 999 and gid 999")
}

func (s *hybridUserImportSuite) TestImportHybridUserData(c *C) {
	base := c.MkDir()
	writeTempFile(c, filepath.Join(base, "etc/passwd"), `
root:x:0:0:root:/root:/bin/bash
lxd:x:964:984::/var/snap/lxd/common/lxd:/bin/false
`)

	writeTempFile(c, filepath.Join(base, "etc/group"), `
root:x:0:
wheel:x:998:
docker:x:968:
lxd:x:964:
sudo:x:959:
admin:x:960:
`)

	writeTempFile(c, filepath.Join(base, "etc/shadow"), `
root:!:19836:0:99999:7:::
lxd:!:19838:0:99999:7:::
`)

	writeTempFile(c, filepath.Join(base, "etc/gshadow"), `
wheel:!::
root:::
docker:!::
lxd:!::
sudo:!::
admin:!::
`)

	hybrid := c.MkDir()
	writeTempFile(c, filepath.Join(hybrid, "etc/passwd"), `
root:x:0:0:root:/root:/bin/bash
user1:x:1000:1000:user1:/home/user1:/bin/bash
user2:x:1001:1001:user2:/home/user2:/usr/bin/fish
user3:x:999:999:user3:/home/user3:/usr/bin/zsh
`)

	writeTempFile(c, filepath.Join(hybrid, "etc/group"), `
root:x:0:
wheel:x:898:user1,user2,user3
docker:x:868:user2
lxd:x:864:user1
sudo:x:859:user1
admin:x:860:user2
user1:x:1000:
user2:x:1001:
user3:x:999:
`)

	writeTempFile(c, filepath.Join(hybrid, "etc/shadow"), `
root:d41d8cd98f00b204e9800998ecf8427e:19836:0:99999:7:::
user1:28a4517315bfa7bda8fdcfb2f1f1d042:19837:0:99999:7:::
user2:5177bcdd67b77a393852bb5ae47ee416:19838:0:99999:7:::
user3:028deb5e54d669e73be35cb5a96eb35b:19839:0:99999:7:::
`)

	writeTempFile(c, filepath.Join(hybrid, "etc/gshadow"), `
wheel:!::user1,user2,user3
root:!::
docker:!::user2
lxd:!::user1
sudo:!::user1
admin:!::user2
user1:!::
user2:!::
user3:!::
`)

	// user1 should be imported since it is in the sudo group. user1 should be
	// added to the wheel, lxd, and sudo groups.
	//
	// user2 should be imported since it is in the admin group. user2 should be
	// added to the wheel, admin and docker groups.
	//
	// user3 should not be imported, since it is not in either the sudo or admin
	// groups.
	err := importHybridUserData(hybrid, base)
	c.Assert(err, IsNil)

	output := filepath.Join(dirs.SnapRunDir, "hybrid-users")

	assertLinesInFile(c, filepath.Join(output, "passwd"), []string{
		"root:x:0:0:root:/root:",
		"lxd:x:964:984::/var/snap/lxd/common/lxd:/bin/false",
		"user1:x:1000:1000:user1:/home/user1:",
		"user2:x:1001:1001:user2:/home/user2:",
	})
	assertFileHasPermissions(c, filepath.Join(output, "passwd"), 0644)

	assertLinesInFile(c, filepath.Join(output, "group"), []string{
		"root:x:0:",
		"wheel:x:998:user1,user2",
		"docker:x:968:user2",
		"lxd:x:964:user1",
		"sudo:x:959:user1",
		"admin:x:960:user2",
		"user1:x:1000:",
		"user2:x:1001:",
	})
	assertFileHasPermissions(c, filepath.Join(output, "group"), 0644)

	assertLinesInFile(c, filepath.Join(output, "shadow"), []string{
		"root:d41d8cd98f00b204e9800998ecf8427e:19836:0:99999:7:::",
		"lxd:!:19838:0:99999:7:::",
		"user1:28a4517315bfa7bda8fdcfb2f1f1d042:19837:0:99999:7:::",
		"user2:5177bcdd67b77a393852bb5ae47ee416:19838:0:99999:7:::",
	})
	assertFileHasPermissions(c, filepath.Join(output, "shadow"), 0600)

	assertLinesInFile(c, filepath.Join(output, "gshadow"), []string{
		"wheel:!::user1,user2",
		"root:!::",
		"docker:!::user2",
		"lxd:!::user1",
		"sudo:!::user1",
		"admin:!::user2",
		"user1:!::",
		"user2:!::",
	})
	assertFileHasPermissions(c, filepath.Join(output, "gshadow"), 0600)

}
