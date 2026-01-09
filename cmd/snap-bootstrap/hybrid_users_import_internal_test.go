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
	err := os.MkdirAll(filepath.Dir(path), 0o755)
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
noshell:x:1003:1003:noshell:/home/noshell

invalid-line:
invalid-line
user4:x:invalid-uid:1003:user4:/home/user4:/usr/bin/zsh
user5:x:1004:invalid-gid:user5:/home/user5:/usr/bin/zsh
`)

	writeTempFile(c, filepath.Join(dir, "etc/group"), `
user1:x:1000:
user2:x:1001:
user3:x:1002:
noshell:x:1003:
wheel:x:998:user1,user2
docker:x:968:user2
lxd:x:964:user1,user3
sudo:x:959:user1,user2,noshell

invalid-line:
invalid-line
`)

	writeTempFile(c, filepath.Join(dir, "etc/shadow"), `
root:d41d8cd98f00b204e9800998ecf8427e:19836:0:99999:7:::
user1:28a4517315bfa7bda8fdcfb2f1f1d042:19837:0:99999:7:::
user2:5177bcdd67b77a393852bb5ae47ee416:19838:0:99999:7:::
user3:028deb5e54d669e73be35cb5a96eb35b:19839:0:99999:7:::
noshell:1cc7b5ea6a765492910b611f5760929f:19840:0:99999:7:::

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
			shell:       "/bin/bash",
			groups:      nil,
			passwdEntry: "root:x:0:0:root:/root:/bin/bash",
			shadowEntry: "root:d41d8cd98f00b204e9800998ecf8427e:19836:0:99999:7:::",
		},
		"user1": {
			name:        "user1",
			uid:         1000,
			gid:         1000,
			shell:       "/bin/bash",
			groups:      []string{"wheel", "lxd", "sudo"},
			passwdEntry: "user1:x:1000:1000:user1:/home/user1:/bin/bash",
			shadowEntry: "user1:28a4517315bfa7bda8fdcfb2f1f1d042:19837:0:99999:7:::",
		},
		"user2": {
			name:        "user2",
			uid:         1001,
			gid:         1001,
			shell:       "/usr/bin/fish",
			groups:      []string{"wheel", "docker", "sudo"},
			passwdEntry: "user2:x:1001:1001:user2:/home/user2:/usr/bin/fish",
			shadowEntry: "user2:5177bcdd67b77a393852bb5ae47ee416:19838:0:99999:7:::",
		},
		"user3": {
			name:        "user3",
			uid:         1002,
			gid:         1002,
			shell:       "/usr/bin/zsh",
			groups:      []string{"lxd"},
			passwdEntry: "user3:x:1002:1002:user3:/home/user3:/usr/bin/zsh",
			shadowEntry: "user3:028deb5e54d669e73be35cb5a96eb35b:19839:0:99999:7:::",
		},
		// we handle importing users that omit the last colon in the passwd
		// entry
		"noshell": {
			name:        "noshell",
			uid:         1003,
			gid:         1003,
			shell:       "",
			groups:      []string{"sudo"},
			passwdEntry: "noshell:x:1003:1003:noshell:/home/noshell",
			shadowEntry: "noshell:1cc7b5ea6a765492910b611f5760929f:19840:0:99999:7:::",
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
			shell:       "/bin/bash",
			groups:      []string{"wheel", "lxd", "sudo"},
			passwdEntry: "user1:x:1000:1000:user1:/home/user1:/bin/bash",
			shadowEntry: "user1:28a4517315bfa7bda8fdcfb2f1f1d042:19837:0:99999:7:::",
		},
	})
}

func (s *hybridUserImportSuite) TestFilterUsers(c *C) {
	users := map[string]user{
		"root": {
			name:  "root",
			uid:   0,
			gid:   0,
			shell: "/bin/sh",
		},
		"user1": {
			name:   "user1",
			uid:    1000,
			gid:    1000,
			shell:  "/bin/bash",
			groups: []string{"wheel", "lxd", "sudo"},
		},
		"user2": {
			name:   "user2",
			uid:    1001,
			gid:    1001,
			shell:  "/bin/bash",
			groups: []string{"wheel", "docker", "admin"},
		},
		"user3": {
			name:   "user3",
			uid:    1002,
			gid:    1002,
			shell:  "/bin/bash",
			groups: []string{"lxd"},
		},
		"uid-conflicting": {
			name:   "uid-conflicting",
			uid:    2001,
			gid:    2020,
			shell:  "/bin/bash",
			groups: []string{"sudo"},
		},
		"gid-conflicting": {
			name:   "uid-conflicting",
			uid:    2020,
			gid:    2002,
			shell:  "/bin/bash",
			groups: []string{"sudo"},
		},
		"system-gid": {
			name:  "system-gid",
			uid:   1002,
			gid:   998,
			shell: "/bin/bash",
		},
		"lxd": {
			name:  "lxd",
			uid:   999,
			gid:   999,
			shell: "/bin/bash",
		},
		"noshell": {
			name:   "noshell",
			uid:    1003,
			gid:    1003,
			groups: []string{"sudo"},
		},
		"nologin": {
			name:   "nologin",
			uid:    1004,
			gid:    1004,
			shell:  "/sbin/nologin",
			groups: []string{"sudo"},
		},
		"name-conflicting": {
			name:   "name-conflicting",
			uid:    1005,
			gid:    1005,
			groups: []string{"sudo"},
		},
	}

	sourceUsers := map[string]user{
		"root": {
			name: "root",
			uid:  0,
			gid:  0,
		},
		"ids-conflicting": {
			name: "ids-conflicting",
			uid:  2001,
			gid:  2001,
		},
		"name-conflicting": {
			name: "name-conflicting",
			uid:  2003,
			gid:  2003,
		},
	}

	sourceGroups := map[string]group{
		"root": {
			name: "root",
			gid:  0,
		},
		"ids-conflicting": {
			name: "ids-conflicting",
			gid:  2002,
		},
	}

	loginShells := []string{"/bin/bash", "/bin/sh"}

	targets := []string{"admin", "sudo"}
	filter := userFilter(targets, sourceUsers, sourceGroups, loginShells)
	c.Assert(filter(users["root"]), Equals, true)
	c.Assert(filter(users["user1"]), Equals, true)
	c.Assert(filter(users["user2"]), Equals, true)
	c.Assert(filter(users["noshell"]), Equals, true)

	c.Assert(filter(users["user3"]), Equals, false)
	c.Assert(filter(users["uid-conflicting"]), Equals, false)
	c.Assert(filter(users["gid-conflicting"]), Equals, false)
	c.Assert(filter(users["system-gid"]), Equals, false)
	c.Assert(filter(users["lxd"]), Equals, false)
	c.Assert(filter(users["nologin"]), Equals, false)
	c.Assert(filter(users["name-conflicting"]), Equals, false)
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
		"conflict": {
			name: "conflict",
			gid:  1003,
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

	baseGroups := map[string]group{
		"conflict": {
			name:         "conflict",
			gid:          2001,
			users:        nil,
			groupEntry:   "conflict:x:0:",
			gshadowEntry: "conflict:!::",
		},
	}

	filter := groupFilter(users, baseGroups)

	c.Assert(filter(groups["root"]), Equals, true)
	c.Assert(filter(groups["user1"]), Equals, true)
	c.Assert(filter(groups["user2"]), Equals, true)
	c.Assert(filter(groups["user3"]), Equals, false)
	c.Assert(filter(groups["conflict"]), Equals, false)
	c.Assert(filter(groups["docker"]), Equals, false)
}

func (s *hybridUserImportSuite) TestMergeAndWriteGroupFiles(c *C) {
	sourceGroups := map[string]group{
		"root": {
			name:         "root",
			gid:          0,
			users:        nil,
			groupEntry:   "root:x:0:",
			gshadowEntry: "root:!::",
		},
		"wheel": {
			name:         "wheel",
			gid:          998,
			users:        []string{"user1"},
			groupEntry:   "wheel:x:998:user1",
			gshadowEntry: "wheel:!::",
		},
		"docker": {
			name:         "docker",
			gid:          968,
			users:        nil,
			groupEntry:   "docker:x:968:",
			gshadowEntry: "docker:!::",
		},
		"lxd": {
			name:       "lxd",
			gid:        964,
			users:      nil,
			groupEntry: "lxd:x:964:",

			// note that this is intentionally empty
			gshadowEntry: "",
		},
		"sudo": {
			name:         "sudo",
			gid:          959,
			users:        nil,
			groupEntry:   "sudo:x:959:",
			gshadowEntry: "sudo:!::",
		},
		"nobody": {
			name:         "nobody",
			gid:          65534,
			users:        nil,
			groupEntry:   "nobody:x:65534:",
			gshadowEntry: "nobody:!::",
		},
	}

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

	dest := c.MkDir()
	err := mergeAndWriteGroupFiles(sourceGroups, users, groups, dest)
	c.Assert(err, IsNil)

	assertLinesInFile(c, filepath.Join(dest, "group"), []string{
		"root:x:0:",
		"wheel:x:998:user1,user2",
		"docker:x:968:user2",
		"lxd:x:964:user1",
		"sudo:x:959:user1,user2",
		"user1:x:1000:",
		"user2:x:1001:",
		"nobody:x:65534:",
	})
	assertFileHasPermissions(c, filepath.Join(dest, "group"), 0644)

	assertLinesInFile(c, filepath.Join(dest, "gshadow"), []string{
		"root:!::",
		"wheel:!::user1,user2",
		"docker:!::user2",
		"sudo:!::user1,user2",
		"user1:!::",
		"user2:!::",
		"nobody:!::",
	})
	assertFileHasPermissions(c, filepath.Join(dest, "gshadow"), 0600)
}

func (s *hybridUserImportSuite) TestMergeAndWriteGroupFilesSharedPrimary(c *C) {
	sourceGroups := map[string]group{
		"root": {
			name:         "root",
			gid:          0,
			users:        nil,
			groupEntry:   "root:x:0:",
			gshadowEntry: "root:!::",
		},
		"wheel": {
			name:         "wheel",
			gid:          998,
			users:        nil,
			groupEntry:   "wheel:x:998:",
			gshadowEntry: "wheel:!::",
		},
		"docker": {
			name:         "docker",
			gid:          968,
			users:        nil,
			groupEntry:   "docker:x:968:",
			gshadowEntry: "docker:!::",
		},
		"lxd": {
			name:       "lxd",
			gid:        964,
			users:      nil,
			groupEntry: "lxd:x:964:",

			// note that this is intentionally empty
			gshadowEntry: "",
		},
		"sudo": {
			name:         "sudo",
			gid:          959,
			users:        nil,
			groupEntry:   "sudo:x:959:",
			gshadowEntry: "sudo:!::",
		},
	}

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

	dest := c.MkDir()
	err := mergeAndWriteGroupFiles(sourceGroups, users, groups, dest)
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

func (s *hybridUserImportSuite) TestMergeAndWriteUserFiles(c *C) {
	sourceUsers := map[string]user{
		"root": {
			name:        "root",
			uid:         0,
			gid:         0,
			groups:      nil,
			passwdEntry: "root:x:0:0:root:/root:/bin/bash",
			shadowEntry: "root:!:19836:0:99999:7:::",
		},
		"user3": {
			name:        "user3",
			uid:         1011,
			gid:         1011,
			groups:      nil,
			passwdEntry: "user3:x:1011:1011:user3:/home/user3:/bin/bash",
			shadowEntry: "user3:dc9b7b7b631aadd960231f4880923d0f:19839:0:99999:7:::",
		},
		"lxd": {
			name:        "lxd",
			uid:         964,
			gid:         964,
			groups:      nil,
			passwdEntry: "lxd:x:964:984::/var/snap/lxd/common/lxd:/bin/false",
			shadowEntry: "lxd:!:19838:0:99999:7:::",
		},
	}

	// root will replace the original root. user2 and noshell will be directly
	// imported.
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
		// we handle importing users that omit the last colon in the passwd
		// entry
		"noshell": {
			name:        "noshell",
			uid:         1002,
			gid:         1002,
			groups:      []string{"sudo"},
			passwdEntry: "noshell:x:1002:1002:noshell:/home/noshell",
			shadowEntry: "noshell:1cc7b5ea6a765492910b611f5760929f:19839:0:99999:7:::",
		},
	}

	dest := c.MkDir()
	err := mergeAndWriteUserFiles(sourceUsers, users, dest)
	c.Assert(err, IsNil)

	// note that the imported users had their default shells changed to
	// /bin/bash, since we can't guarantee that the original shells are
	// installed on the system that we're importing into.
	assertLinesInFile(c, filepath.Join(dest, "passwd"), []string{
		"root:x:0:0:root:/root:/bin/bash",
		"lxd:x:964:984::/var/snap/lxd/common/lxd:/bin/false",
		"user1:x:1000:1000:user1:/home/user1:/bin/bash",
		"user2:x:1001:1001:user2:/home/user2:/bin/bash",
		"user3:x:1011:1011:user3:/home/user3:/bin/bash",
		"noshell:x:1002:1002:noshell:/home/noshell:/bin/bash",
	})
	assertFileHasPermissions(c, filepath.Join(dest, "passwd"), 0644)

	assertLinesInFile(c, filepath.Join(dest, "shadow"), []string{
		"root:d41d8cd98f00b204e9800998ecf8427e:19836:0:99999:7:::",
		"lxd:!:19838:0:99999:7:::",
		"user1:28a4517315bfa7bda8fdcfb2f1f1d042:19837:0:99999:7:::",
		"user2:5177bcdd67b77a393852bb5ae47ee416:19838:0:99999:7:::",
		"user3:dc9b7b7b631aadd960231f4880923d0f:19839:0:99999:7:::",
		"noshell:1cc7b5ea6a765492910b611f5760929f:19839:0:99999:7:::",
	})
	assertFileHasPermissions(c, filepath.Join(dest, "shadow"), 0600)
}

func (s *hybridUserImportSuite) TestImportHybridUserData(c *C) {
	base := c.MkDir()
	writeTempFile(c, filepath.Join(base, "etc/passwd"), `
root:x:0:0:root:/root:/bin/bash
lxd:x:964:984::/var/snap/lxd/common/lxd:/bin/false
conflict:x:2002:2002::/home/conflict:/bin/bash
`)

	writeTempFile(c, filepath.Join(base, "etc/group"), `
root:x:0:
wheel:x:998:
docker:x:968:
lxd:x:964:
sudo:x:959:
admin:x:960:
conflict:x:2002:
`)

	writeTempFile(c, filepath.Join(base, "etc/shadow"), `
root:!:19836:0:99999:7:::
lxd:!:19838:0:99999:7:::
conflict:!:19839:0:99999:7:::
`)

	writeTempFile(c, filepath.Join(base, "etc/gshadow"), `
wheel:!::
root:::
docker:!::
lxd:!::
sudo:!::
admin:!::
conflict:!::
`)

	hybrid := c.MkDir()
	writeTempFile(c, filepath.Join(hybrid, "etc/passwd"), `
root:x:0:0:root:/root:/bin/bash
user1:x:1000:1000:user1:/home/user1:/bin/bash
user2:x:1001:1001:user2:/home/user2:/usr/bin/fish
user3:x:1002:1002:user3:/home/user3:/usr/bin/zsh
nologin:x:1003:1003:nologin:/home/nologin:/sbin/nologin
conflict-uid:x:2002:2020:conflict-uid:/home/conflict-uid:/bin/bash
conflict-gid:x:2020:2002:conflict-gid:/home/conflict-gid:/bin/bash
system:x:999:999:system:/home/system:/bin/bash
`)

	writeTempFile(c, filepath.Join(hybrid, "etc/group"), `
root:x:0:
wheel:x:898:user1,user2,user3
docker:x:868:user2
lxd:x:864:user1
sudo:x:859:user1,nologin
admin:x:860:user2
user1:x:1000:
user2:x:1001:
user3:x:1002:
nologin:x:1003:
conflict-uid:x:2020:
conflict-gid:x:2002:
system:x:999:
`)

	writeTempFile(c, filepath.Join(hybrid, "etc/shadow"), `
root:d41d8cd98f00b204e9800998ecf8427e:19836:0:99999:7:::
user1:28a4517315bfa7bda8fdcfb2f1f1d042:19837:0:99999:7:::
user2:5177bcdd67b77a393852bb5ae47ee416:19838:0:99999:7:::
user3:028deb5e54d669e73be35cb5a96eb35b:19839:0:99999:7:::
nologin:7c9369afc7deaff45e8db9477718df7b:19840:0:99999:7:::
conflict-uid:d741dddf79cfc8677e3d0c8129e803f5:19840:0:99999:7:::
conflict-gid:591713280c6992a20c64abb170ac175c:19841:0:99999:7:::
system:bfcc9da4f2e1d313c63cd0a4ee7604e9:19850:0:99999:7:::
`)

	writeTempFile(c, filepath.Join(hybrid, "etc/gshadow"), `
wheel:!::user1,user2,user3
root:!::
docker:!::user2
lxd:!::user1
sudo:!::user1,nologin
admin:!::user2
user1:!::
user2:!::
user3:!::
nologin:!::
conflict-uid:!::
conflict-gid:!::
system:!::
`)

	writeTempFile(c, filepath.Join(hybrid, "etc/shells"), `# /etc/shells: valid login shells
/bin/sh
/bin/bash
/usr/bin/fish
/usr/bin/zsh
`)

	// user1 should be imported since it is in the sudo group. user1 should be
	// added to the wheel, lxd, and sudo groups.
	//
	// user2 should be imported since it is in the admin group. user2 should be
	// added to the wheel, admin and docker groups.
	//
	// user3 should not be imported, since it is not in either the sudo or admin
	// groups.
	//
	// system should not be imported, since it has a uid/gid < 1000.
	//
	// conflict-uid and conflict-gid should not be imported, since they share a
	// uid/gid with a user and group from the base.
	//
	// nologin should not be imported, since it has a shell that is not in the
	// list of valid login shells in the hybrid system. this is despite it being
	// a non-system user that is in the sudo group.
	err := importHybridUserData(hybrid, base)
	c.Assert(err, IsNil)

	output := filepath.Join(dirs.SnapRunDir, "hybrid-users")

	assertLinesInFile(c, filepath.Join(output, "passwd"), []string{
		"root:x:0:0:root:/root:/bin/bash",
		"lxd:x:964:984::/var/snap/lxd/common/lxd:/bin/false",
		"user1:x:1000:1000:user1:/home/user1:/bin/bash",
		"user2:x:1001:1001:user2:/home/user2:/bin/bash",
		"conflict:x:2002:2002::/home/conflict:/bin/bash",
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
		"conflict:x:2002:",
	})
	assertFileHasPermissions(c, filepath.Join(output, "group"), 0644)

	assertLinesInFile(c, filepath.Join(output, "shadow"), []string{
		"root:d41d8cd98f00b204e9800998ecf8427e:19836:0:99999:7:::",
		"lxd:!:19838:0:99999:7:::",
		"user1:28a4517315bfa7bda8fdcfb2f1f1d042:19837:0:99999:7:::",
		"user2:5177bcdd67b77a393852bb5ae47ee416:19838:0:99999:7:::",
		"conflict:!:19839:0:99999:7:::",
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
		"conflict:!::",
	})
	assertFileHasPermissions(c, filepath.Join(output, "gshadow"), 0600)

}

func (s *hybridUserImportSuite) TestParseShells(c *C) {
	dir := c.MkDir()
	writeTempFile(c, filepath.Join(dir, "etc/shells"), `
# comment
/bin/sh
  # indented comment
/bin/bash

/usr/bin/zsh
/usr/bin/fish#comment
#/bin/dash#comment
/bin/ksh # comment # comment
#/bin/tcsh
`)

	shells, err := parseShells(dir)
	c.Assert(err, IsNil)
	c.Assert(shells, DeepEquals, []string{
		"/bin/sh",
		"/bin/bash",
		"/usr/bin/zsh",
		"/usr/bin/fish",
		"/bin/ksh",
	})
}

func (s *hybridUserImportSuite) TestParseShellsWithDefaults(c *C) {
	defaults := []string{"/bin/dash", "/bin/bash"}
	shells := parseShellsWithDefaults(c.MkDir(), defaults)
	c.Assert(shells, DeepEquals, defaults)
}
