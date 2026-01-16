// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package requestprompts_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
	"unsafe"

	. "gopkg.in/check.v1"

	"golang.org/x/sys/unix"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces/prompting"
	prompting_errors "github.com/snapcore/snapd/interfaces/prompting/errors"
	"github.com/snapcore/snapd/interfaces/prompting/internal/maxidmmap"
	"github.com/snapcore/snapd/interfaces/prompting/patterns"
	"github.com/snapcore/snapd/interfaces/prompting/requestprompts"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testtime"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timeutil"
)

func Test(t *testing.T) { TestingT(t) }

type noticeInfo struct {
	promptID prompting.IDType
	data     map[string]string
}

type requestpromptsSuite struct {
	defaultNotifyPrompt func(userID uint32, promptID prompting.IDType, data map[string]string) error
	defaultUser         uint32
	promptNotices       []*noticeInfo

	tmpdir               string
	legacyMaxIDPath      string
	maxIDPath            string
	requestIDMapFilepath string
}

var _ = Suite(&requestpromptsSuite{})

func (s *requestpromptsSuite) SetUpTest(c *C) {
	s.defaultUser = 1000
	s.defaultNotifyPrompt = func(userID uint32, promptID prompting.IDType, data map[string]string) error {
		c.Check(userID, Equals, s.defaultUser)
		info := &noticeInfo{
			promptID: promptID,
			data:     data,
		}
		s.promptNotices = append(s.promptNotices, info)
		return nil
	}
	s.promptNotices = make([]*noticeInfo, 0)
	s.tmpdir = c.MkDir()
	dirs.SetRootDir(s.tmpdir)
	s.legacyMaxIDPath = filepath.Join(dirs.SnapRunDir, "request-prompt-max-id")
	s.maxIDPath = filepath.Join(dirs.SnapInterfacesRequestsRunDir, "request-prompt-max-id")
	s.requestIDMapFilepath = filepath.Join(dirs.SnapInterfacesRequestsRunDir, "request-id-mapping.json")
}

func newRequestWithReplyChan(key string) (req *prompting.Request, replyChan chan []string) {
	replyChan = make(chan []string, 1)
	req = &prompting.Request{
		Key: key,
		Reply: func(allowedPerms []string) error {
			replyChan <- allowedPerms
			return nil
		},
	}
	return req, replyChan
}

func (s *requestpromptsSuite) TestNew(c *C) {
	notifyPrompt := func(userID uint32, promptID prompting.IDType, data map[string]string) error {
		c.Fatalf("unexpected notice with userID %d and ID %016X", userID, promptID)
		return nil
	}
	pdb, err := requestprompts.New(notifyPrompt)
	c.Assert(err, IsNil)
	defer pdb.Close()
	c.Check(pdb.PerUser(), HasLen, 0)
	nextID, err := pdb.NextID()
	c.Check(err, IsNil)
	c.Check(nextID, Equals, prompting.IDType(1))
}

func (s *requestpromptsSuite) TestNewValidMaxID(c *C) {
	c.Assert(os.MkdirAll(dirs.SnapInterfacesRequestsRunDir, 0o777), IsNil)

	notifyPrompt := func(userID uint32, promptID prompting.IDType, data map[string]string) error {
		c.Fatalf("unexpected notice with userID %d and ID %016X", userID, promptID)
		return nil
	}
	for _, testCase := range []struct {
		initial uint64
		nextID  prompting.IDType
	}{
		{
			0,
			1,
		},
		{
			1,
			2,
		},
		{
			0x1000000000000001,
			0x1000000000000002,
		},
		{
			0x0123456789ABCDEF,
			0x0123456789ABCDF0,
		},
		{
			0xDEADBEEFDEADBEEF,
			0xDEADBEEFDEADBEF0,
		},
	} {
		var initialData [8]byte
		*(*uint64)(unsafe.Pointer(&initialData[0])) = testCase.initial
		c.Assert(osutil.AtomicWriteFile(s.maxIDPath, initialData[:], 0o600, 0), IsNil)
		pdb, err := requestprompts.New(notifyPrompt)
		c.Assert(err, IsNil)
		defer pdb.Close()
		s.checkWrittenMaxID(c, testCase.initial)
		nextID, err := pdb.NextID()
		c.Check(err, IsNil)
		c.Check(nextID, Equals, testCase.nextID)
		s.checkWrittenMaxID(c, testCase.initial+1)
	}
}

func (s *requestpromptsSuite) TestNewInvalidMaxID(c *C) {
	notifyPrompt := func(userID uint32, promptID prompting.IDType, data map[string]string) error {
		c.Fatalf("unexpected notice with userID %d and ID %016X", userID, promptID)
		return nil
	}

	// First try with no existing max ID file
	pdb, err := requestprompts.New(notifyPrompt)
	c.Assert(err, IsNil)
	defer pdb.Close()
	s.checkWrittenMaxID(c, 0)
	nextID, err := pdb.NextID()
	c.Check(err, IsNil)
	c.Check(nextID, Equals, prompting.IDType(1))
	s.checkWrittenMaxID(c, 1)

	// Now try with various invalid max ID files
	for _, initial := range [][]byte{
		[]byte(""),
		[]byte("foo"),
		[]byte("1234"),
		[]byte("1234567"),
		[]byte("123456789"),
	} {
		c.Assert(osutil.AtomicWriteFile(s.maxIDPath, initial, 0o600, 0), IsNil)
		pdb, err := requestprompts.New(notifyPrompt)
		c.Assert(err, IsNil)
		defer pdb.Close()
		s.checkWrittenMaxID(c, 0)
		nextID, err := pdb.NextID()
		c.Check(err, IsNil)
		c.Check(nextID, Equals, prompting.IDType(1))
		s.checkWrittenMaxID(c, 1)
	}
}

func (s *requestpromptsSuite) TestNewNextIDUniqueIDs(c *C) {
	c.Assert(os.MkdirAll(dirs.SnapInterfacesRequestsRunDir, 0o755), IsNil)

	var initialMaxID uint64 = 42
	var initialData [8]byte
	*(*uint64)(unsafe.Pointer(&initialData[0])) = initialMaxID
	c.Assert(osutil.AtomicWriteFile(s.maxIDPath, initialData[:], 0600, 0), IsNil)

	pdb1, err := requestprompts.New(s.defaultNotifyPrompt)
	c.Assert(err, IsNil)
	defer pdb1.Close()
	expectedID := initialMaxID + 1
	nextID, err := pdb1.NextID()
	c.Check(err, IsNil)
	c.Check(nextID, Equals, prompting.IDType(expectedID))
	s.checkWrittenMaxID(c, expectedID)

	// New prompt DB should start where existing one left off
	pdb2, err := requestprompts.New(s.defaultNotifyPrompt)
	c.Assert(err, IsNil)
	defer pdb2.Close()
	expectedID++
	nextID, err = pdb2.NextID()
	c.Check(err, IsNil)
	c.Check(nextID, Equals, prompting.IDType(expectedID))

	// Both prompt DBs should be aware of any new IDs created by any others
	expectedID++
	nextID, err = pdb1.NextID()
	c.Check(err, IsNil)
	c.Check(nextID, Equals, prompting.IDType(expectedID))

	expectedID++
	nextID, err = pdb1.NextID()
	c.Check(err, IsNil)
	c.Check(nextID, Equals, prompting.IDType(expectedID))

	expectedID++
	nextID, err = pdb2.NextID()
	c.Check(err, IsNil)
	c.Check(nextID, Equals, prompting.IDType(expectedID))

	// For the checks above to have passed, incremented IDs must have been
	// written to disk, but check now anyway. Theoretically, checking disk
	// earlier might have caused mmaped data to be flushed, so wait until now.
	s.checkWrittenMaxID(c, expectedID)
}

func (s *requestpromptsSuite) checkWrittenMaxID(c *C, id uint64) {
	data, err := os.ReadFile(s.maxIDPath)
	c.Assert(err, IsNil)
	c.Assert(data, HasLen, 8)
	writtenID := *(*uint64)(unsafe.Pointer(&data[0]))
	c.Assert(writtenID, Equals, id)
}

func (s *requestpromptsSuite) checkWrittenRequestMap(c *C, requestMap map[string]requestprompts.RequestMapEntry) {
	data, err := os.ReadFile(s.requestIDMapFilepath)
	c.Assert(err, IsNil)
	var mapping requestprompts.RequestMappingJSON
	err = json.Unmarshal(data, &mapping)
	c.Assert(err, IsNil, Commentf("data: %v", string(data)))
	c.Check(mapping.RequestMap, DeepEquals, requestMap)
}

func (s *requestpromptsSuite) TestNewNextIDCompatibility(c *C) {
	c.Assert(os.MkdirAll(dirs.SnapRunDir, 0o755), IsNil)

	var initialMaxID uint64 = 42
	var initialData [8]byte
	*(*uint64)(unsafe.Pointer(&initialData[0])) = initialMaxID
	c.Assert(osutil.AtomicWriteFile(s.legacyMaxIDPath, initialData[:], 0600, 0), IsNil)

	pdb1, err := requestprompts.New(s.defaultNotifyPrompt)
	c.Assert(err, IsNil)
	defer pdb1.Close()
	expectedID := initialMaxID + 1
	nextID, err := pdb1.NextID()
	c.Check(err, IsNil)
	c.Check(nextID, Equals, prompting.IDType(expectedID))
	s.checkWrittenMaxID(c, expectedID)

	// Set maxIDPath to legacyMaxIDPath so checkWrittenID checks legacy path.
	// Since the legacy path existed, it should have been hard linked to the
	// new path, and it should have been updated as the max ID updated.
	restore := testutil.Mock(&s.maxIDPath, s.legacyMaxIDPath)
	defer restore()
	s.checkWrittenMaxID(c, expectedID)
}

func (s *requestpromptsSuite) TestAddOrMergeNonMerges(c *C) {
	pdb, err := requestprompts.New(s.defaultNotifyPrompt)
	c.Assert(err, IsNil)
	defer pdb.Close()

	metadataTemplate := prompting.Metadata{
		User:      s.defaultUser,
		Snap:      "nextcloud",
		PID:       1234,
		Cgroup:    "some-cgroup-name",
		Interface: "home",
	}
	path := "/home/test/Documents/foo.txt"
	permissions := []string{"read", "write", "execute"}

	req1 := &prompting.Request{Key: "fake:1"}
	req2 := &prompting.Request{Key: "fake:2"}
	req3 := &prompting.Request{Key: "fake:3"}
	req4 := &prompting.Request{Key: "fake:4"}
	req5 := &prompting.Request{Key: "fake:5"}
	req6 := &prompting.Request{Key: "fake:6"}

	clientActivity := false // doesn't matter if it's true or false for this test
	stored, err := pdb.Prompts(metadataTemplate.User, clientActivity)
	c.Assert(err, IsNil)
	c.Assert(stored, IsNil)

	metadata := metadataTemplate
	before := time.Now()
	prompt1, merged, err := pdb.AddOrMerge(&metadata, path, permissions, permissions, req1)
	c.Assert(err, IsNil)
	after := time.Now()
	c.Assert(merged, Equals, false)

	c.Check(prompt1.Timestamp.After(before), Equals, true)
	c.Check(prompt1.Timestamp.Before(after), Equals, true)

	c.Check(prompt1.Snap, Equals, metadata.Snap)
	c.Check(prompt1.PID, Equals, metadata.PID)
	c.Check(prompt1.Cgroup, Equals, metadata.Cgroup)
	c.Check(prompt1.Interface, Equals, metadata.Interface)
	c.Check(prompt1.Constraints.Path(), Equals, path)
	c.Check(prompt1.Constraints.OutstandingPermissions(), DeepEquals, permissions)
	c.Assert(prompt1.Requests(), HasLen, 1)
	c.Check(prompt1.Requests()[0].Key, Equals, "fake:1")

	expectedID := uint64(1)
	expectedMap := map[string]requestprompts.RequestMapEntry{"fake:1": {PromptID: 1, UserID: s.defaultUser}}

	s.checkNewNoticesSimple(c, []prompting.IDType{prompt1.ID}, nil)
	s.checkWrittenMaxID(c, expectedID)
	s.checkWrittenRequestMap(c, expectedMap)

	storedPrompt, err := pdb.PromptWithID(metadata.User, prompt1.ID, clientActivity)
	c.Check(err, IsNil)
	c.Check(storedPrompt, Equals, prompt1)

	stored, err = pdb.Prompts(metadata.User, clientActivity)
	c.Assert(err, IsNil)
	c.Assert(stored, HasLen, 1)
	c.Check(stored[0], Equals, prompt1)

	// Looking up prompt should not record notice
	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)

	// Add second prompt, this time with different snap

	metadata = metadataTemplate
	metadata.Snap = "firefox"
	prompt2, merged, err := pdb.AddOrMerge(&metadata, path, permissions, permissions, req2)
	c.Assert(err, IsNil)
	c.Assert(merged, Equals, false)
	c.Assert(prompt2, Not(Equals), prompt1)

	c.Check(prompt2.Snap, Equals, metadata.Snap)
	c.Check(prompt2.PID, Equals, metadata.PID)
	c.Check(prompt2.Cgroup, Equals, metadata.Cgroup)
	c.Check(prompt2.Interface, Equals, metadata.Interface)
	c.Check(prompt2.Constraints.Path(), Equals, path)
	c.Check(prompt2.Constraints.OutstandingPermissions(), DeepEquals, permissions)

	// Request was added to the requests list
	c.Assert(prompt2.Requests(), HasLen, 1)
	c.Check(prompt2.Requests()[0].Key, Equals, "fake:2")

	// New prompts should record notice
	s.checkNewNoticesSimple(c, []prompting.IDType{prompt2.ID}, nil)
	// New prompts should advance the max ID
	expectedID++
	expectedMap["fake:2"] = requestprompts.RequestMapEntry{PromptID: 2, UserID: s.defaultUser}
	s.checkWrittenMaxID(c, expectedID)
	s.checkWrittenRequestMap(c, expectedMap)

	storedPrompt, err = pdb.PromptWithID(metadata.User, prompt2.ID, clientActivity)
	c.Check(err, IsNil)
	c.Check(storedPrompt, Equals, prompt2)

	stored, err = pdb.Prompts(metadata.User, clientActivity)
	c.Assert(err, IsNil)
	c.Assert(stored, HasLen, 2)
	c.Check(stored[0], Equals, prompt1)
	c.Check(stored[1], Equals, prompt2)

	// Looking up prompt should not record notice
	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)

	// Add third prompt, this time with different PID but same Cgroup

	metadata = metadataTemplate
	metadata.PID = 1337
	prompt3, merged, err := pdb.AddOrMerge(&metadata, path, permissions, permissions, req3)
	c.Assert(err, IsNil)
	c.Check(merged, Equals, false)
	c.Check(prompt3, Not(Equals), prompt1)
	c.Check(prompt3, Not(Equals), prompt2)

	c.Check(prompt3.Snap, Equals, metadata.Snap)
	c.Check(prompt3.PID, Equals, metadata.PID)
	c.Check(prompt3.Cgroup, Equals, metadata.Cgroup)
	c.Check(prompt3.Interface, Equals, metadata.Interface)
	c.Check(prompt3.Constraints.Path(), Equals, path)
	c.Check(prompt3.Constraints.OutstandingPermissions(), DeepEquals, permissions)
	c.Assert(prompt3.Requests(), HasLen, 1)
	c.Check(prompt3.Requests()[0].Key, Equals, "fake:3")

	// New prompts should record notice
	s.checkNewNoticesSimple(c, []prompting.IDType{prompt3.ID}, nil)
	// New prompts should advance the max ID
	expectedID++
	expectedMap["fake:3"] = requestprompts.RequestMapEntry{PromptID: 3, UserID: s.defaultUser}
	s.checkWrittenMaxID(c, expectedID)
	s.checkWrittenRequestMap(c, expectedMap)

	storedPrompt, err = pdb.PromptWithID(metadata.User, prompt3.ID, clientActivity)
	c.Check(err, IsNil)
	c.Check(storedPrompt, Equals, prompt3)

	stored, err = pdb.Prompts(metadata.User, clientActivity)
	c.Assert(err, IsNil)
	c.Assert(stored, HasLen, 3)
	c.Check(stored[0], Equals, prompt1)
	c.Check(stored[1], Equals, prompt2)
	c.Check(stored[2], Equals, prompt3)

	// Looking up prompt should not record notice
	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)

	// Add fourth prompt, this time with different Cgroup but same PID

	metadata = metadataTemplate
	metadata.Cgroup = "another/cgroup"
	prompt4, merged, err := pdb.AddOrMerge(&metadata, path, permissions, permissions, req4)
	c.Assert(err, IsNil)
	c.Check(merged, Equals, false)
	c.Check(prompt4, Not(Equals), prompt1)
	c.Check(prompt4, Not(Equals), prompt2)
	c.Check(prompt4, Not(Equals), prompt3)

	c.Check(prompt4.Snap, Equals, metadata.Snap)
	c.Check(prompt4.PID, Equals, metadata.PID)
	c.Check(prompt4.Cgroup, Equals, metadata.Cgroup)
	c.Check(prompt4.Interface, Equals, metadata.Interface)
	c.Check(prompt4.Constraints.Path(), Equals, path)
	c.Check(prompt4.Constraints.OutstandingPermissions(), DeepEquals, permissions)
	c.Assert(prompt4.Requests(), HasLen, 1)
	c.Check(prompt4.Requests()[0].Key, Equals, "fake:4")

	// New prompts should record notice
	s.checkNewNoticesSimple(c, []prompting.IDType{prompt4.ID}, nil)
	// New prompts should advance the max ID
	expectedID++
	expectedMap["fake:4"] = requestprompts.RequestMapEntry{PromptID: 4, UserID: s.defaultUser}
	s.checkWrittenMaxID(c, expectedID)
	s.checkWrittenRequestMap(c, expectedMap)

	storedPrompt, err = pdb.PromptWithID(metadata.User, prompt4.ID, clientActivity)
	c.Check(err, IsNil)
	c.Check(storedPrompt, Equals, prompt4)

	stored, err = pdb.Prompts(metadata.User, clientActivity)
	c.Assert(err, IsNil)
	c.Assert(stored, HasLen, 4)
	c.Check(stored[0], Equals, prompt1)
	c.Check(stored[1], Equals, prompt2)
	c.Check(stored[2], Equals, prompt3)
	c.Check(stored[3], Equals, prompt4)

	// Looking up prompt should not record notice
	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)

	// Add fifth prompt, this time with different path

	metadata = metadataTemplate
	path = "/home/test/Documents/other.txt"
	prompt5, merged, err := pdb.AddOrMerge(&metadata, path, permissions, permissions, req5)
	c.Assert(err, IsNil)
	c.Check(merged, Equals, false)
	c.Check(prompt5, Not(Equals), prompt1)
	c.Check(prompt5, Not(Equals), prompt2)
	c.Check(prompt5, Not(Equals), prompt3)
	c.Check(prompt5, Not(Equals), prompt4)

	c.Check(prompt5.Snap, Equals, metadata.Snap)
	c.Check(prompt5.PID, Equals, metadata.PID)
	c.Check(prompt5.Cgroup, Equals, metadata.Cgroup)
	c.Check(prompt5.Interface, Equals, metadata.Interface)
	c.Check(prompt5.Constraints.Path(), Equals, path)
	c.Check(prompt5.Constraints.OutstandingPermissions(), DeepEquals, permissions)
	c.Assert(prompt5.Requests(), HasLen, 1)
	c.Check(prompt5.Requests()[0].Key, Equals, "fake:5")

	// New prompts should record notice
	s.checkNewNoticesSimple(c, []prompting.IDType{prompt5.ID}, nil)
	// New prompts should advance the max ID
	expectedID++
	expectedMap["fake:5"] = requestprompts.RequestMapEntry{PromptID: 5, UserID: s.defaultUser}
	s.checkWrittenMaxID(c, expectedID)
	s.checkWrittenRequestMap(c, expectedMap)

	storedPrompt, err = pdb.PromptWithID(metadata.User, prompt5.ID, clientActivity)
	c.Check(err, IsNil)
	c.Check(storedPrompt, Equals, prompt5)

	stored, err = pdb.Prompts(metadata.User, clientActivity)
	c.Assert(err, IsNil)
	c.Assert(stored, HasLen, 5)
	c.Check(stored[0], Equals, prompt1)
	c.Check(stored[1], Equals, prompt2)
	c.Check(stored[2], Equals, prompt3)
	c.Check(stored[3], Equals, prompt4)
	c.Check(stored[4], Equals, prompt5)

	// Looking up prompt should not record notice
	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)

	// Add sixth prompt, this time with different requested permissions

	metadata = metadataTemplate
	path = "/home/test/Documents/foo.txt"
	requestedPermissions := permissions[:2]
	prompt6, merged, err := pdb.AddOrMerge(&metadata, path, requestedPermissions, permissions, req6)
	c.Assert(err, IsNil)
	c.Check(merged, Equals, false)
	c.Check(prompt6, Not(Equals), prompt1)
	c.Check(prompt6, Not(Equals), prompt2)
	c.Check(prompt6, Not(Equals), prompt3)
	c.Check(prompt6, Not(Equals), prompt4)
	c.Check(prompt6, Not(Equals), prompt5)

	c.Check(prompt6.Snap, Equals, metadata.Snap)
	c.Check(prompt6.PID, Equals, metadata.PID)
	c.Check(prompt6.Cgroup, Equals, metadata.Cgroup)
	c.Check(prompt6.Interface, Equals, metadata.Interface)
	c.Check(prompt6.Constraints.Path(), Equals, path)
	c.Check(prompt6.Constraints.OutstandingPermissions(), DeepEquals, permissions)
	c.Assert(prompt6.Requests(), HasLen, 1)
	c.Check(prompt6.Requests()[0].Key, Equals, "fake:6")

	// New prompts should record notice
	s.checkNewNoticesSimple(c, []prompting.IDType{prompt6.ID}, nil)
	// New prompts should advance the max ID
	expectedID++
	expectedMap["fake:6"] = requestprompts.RequestMapEntry{PromptID: 6, UserID: s.defaultUser}
	s.checkWrittenMaxID(c, expectedID)
	s.checkWrittenRequestMap(c, expectedMap)

	storedPrompt, err = pdb.PromptWithID(metadata.User, prompt6.ID, clientActivity)
	c.Check(err, IsNil)
	c.Check(storedPrompt, Equals, prompt6)

	stored, err = pdb.Prompts(metadata.User, clientActivity)
	c.Assert(err, IsNil)
	c.Assert(stored, HasLen, 6)
	c.Check(stored[0], Equals, prompt1)
	c.Check(stored[1], Equals, prompt2)
	c.Check(stored[2], Equals, prompt3)
	c.Check(stored[3], Equals, prompt4)
	c.Check(stored[4], Equals, prompt5)
	c.Check(stored[5], Equals, prompt6)

	// Looking up prompt should not record notice
	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)
}

func (s *requestpromptsSuite) TestAddOrMergeMerges(c *C) {
	pdb, err := requestprompts.New(s.defaultNotifyPrompt)
	c.Assert(err, IsNil)
	defer pdb.Close()

	metadata := &prompting.Metadata{
		User:      s.defaultUser,
		Snap:      "nextcloud",
		PID:       1234,
		Cgroup:    "some/cgroup/path",
		Interface: "home",
	}
	path := "/home/test/Documents/foo.txt"
	permissions := []string{"read", "write", "execute"}

	req1 := &prompting.Request{Key: "fake:1"}
	req2 := &prompting.Request{Key: "fake:2"}
	req3 := &prompting.Request{Key: "fake:3"}

	clientActivity := false // doesn't matter if it's true or false for this test
	stored, err := pdb.Prompts(metadata.User, clientActivity)
	c.Assert(err, IsNil)
	c.Assert(stored, IsNil)

	before := time.Now()
	prompt1, merged, err := pdb.AddOrMerge(metadata, path, permissions, permissions, req1)
	c.Assert(err, IsNil)
	after := time.Now()
	c.Assert(merged, Equals, false)

	// Request was added to the requests list
	c.Assert(prompt1.Requests(), HasLen, 1)
	c.Check(prompt1.Requests()[0].Key, Equals, "fake:1")

	expectedID := uint64(1)
	expectedMap := map[string]requestprompts.RequestMapEntry{"fake:1": {PromptID: 1, UserID: s.defaultUser}}

	s.checkNewNoticesSimple(c, []prompting.IDType{prompt1.ID}, nil)
	s.checkWrittenMaxID(c, expectedID)
	s.checkWrittenRequestMap(c, expectedMap)

	prompt2, merged, err := pdb.AddOrMerge(metadata, path, permissions, permissions, req2)
	c.Assert(err, IsNil)
	c.Assert(merged, Equals, true)
	c.Assert(prompt2, Equals, prompt1)

	// New request was added to the requests list
	c.Assert(prompt2.Requests(), HasLen, 2)
	c.Check(prompt2.Requests()[0].Key, Equals, "fake:1")
	c.Check(prompt2.Requests()[1].Key, Equals, "fake:2")

	// Merged prompts should re-record notice
	s.checkNewNoticesSimple(c, []prompting.IDType{prompt1.ID}, nil)
	// Merged prompts should not advance the max ID
	s.checkWrittenMaxID(c, expectedID)
	// Merged prompts should create mapping from new request ID to existing prompt ID
	expectedMap["fake:2"] = requestprompts.RequestMapEntry{PromptID: 1, UserID: s.defaultUser}
	s.checkWrittenRequestMap(c, expectedMap)

	// Merged prompts should not affect the original timestamp
	c.Check(prompt1.Timestamp.After(before), Equals, true)
	c.Check(prompt1.Timestamp.Before(after), Equals, true)

	c.Check(prompt1.Snap, Equals, metadata.Snap)
	c.Check(prompt1.PID, Equals, metadata.PID)
	c.Check(prompt1.Cgroup, Equals, metadata.Cgroup)
	c.Check(prompt1.Interface, Equals, metadata.Interface)
	c.Check(prompt1.Constraints.Path(), Equals, path)
	c.Check(prompt1.Constraints.OutstandingPermissions(), DeepEquals, permissions)

	stored, err = pdb.Prompts(metadata.User, clientActivity)
	c.Assert(err, IsNil)
	c.Assert(stored, HasLen, 1)
	c.Check(stored[0], Equals, prompt1)

	storedPrompt, err := pdb.PromptWithID(metadata.User, prompt1.ID, clientActivity)
	c.Check(err, IsNil)
	c.Check(storedPrompt, Equals, prompt1)

	// Looking up prompt should not record notice
	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)

	prompt3, merged, err := pdb.AddOrMerge(metadata, path, permissions, permissions, req3)
	c.Assert(err, IsNil)
	c.Check(merged, Equals, true)
	c.Check(prompt3, Equals, prompt1)

	// New request was added to the requests list
	c.Assert(prompt3.Requests(), HasLen, 3)
	c.Check(prompt3.Requests()[0].Key, Equals, "fake:1")
	c.Check(prompt3.Requests()[1].Key, Equals, "fake:2")
	c.Check(prompt3.Requests()[2].Key, Equals, "fake:3")

	// Merged prompts should re-record notice
	s.checkNewNoticesSimple(c, []prompting.IDType{prompt1.ID}, nil)
	// Merged prompts should not advance the max ID
	s.checkWrittenMaxID(c, expectedID)
	// Merged prompts should create mapping from new request ID to existing prompt ID
	expectedMap["fake:3"] = requestprompts.RequestMapEntry{PromptID: 1, UserID: s.defaultUser}
	s.checkWrittenRequestMap(c, expectedMap)
}

func (s *requestpromptsSuite) TestAddOrMergeDuplicateRequests(c *C) {
	pdb, err := requestprompts.New(s.defaultNotifyPrompt)
	c.Assert(err, IsNil)
	defer pdb.Close()

	metadata := &prompting.Metadata{
		User:      s.defaultUser,
		Snap:      "nextcloud",
		PID:       1234,
		Cgroup:    "some-cgroup-path",
		Interface: "home",
	}
	path := "/home/test/Documents/foo.txt"
	permissions := []string{"read", "write", "execute"}

	req1 := &prompting.Request{Key: "fake:1"}

	clientActivity := false // doesn't matter if it's true or false for this test
	stored, err := pdb.Prompts(metadata.User, clientActivity)
	c.Assert(err, IsNil)
	c.Assert(stored, IsNil)

	prompt1, merged, err := pdb.AddOrMerge(metadata, path, permissions, permissions, req1)
	c.Assert(err, IsNil)
	c.Assert(merged, Equals, false)

	// Request was added to the requests list
	c.Assert(prompt1.Requests(), HasLen, 1)
	c.Check(prompt1.Requests()[0].Key, Equals, "fake:1")

	expectedID := uint64(1)
	expectedMap := map[string]requestprompts.RequestMapEntry{"fake:1": {PromptID: 1, UserID: s.defaultUser}}

	s.checkNewNoticesSimple(c, []prompting.IDType{prompt1.ID}, nil)
	s.checkWrittenMaxID(c, expectedID)
	s.checkWrittenRequestMap(c, expectedMap)

	prompt2, merged, err := pdb.AddOrMerge(metadata, path, permissions, permissions, req1)
	c.Assert(err, IsNil)
	c.Assert(merged, Equals, true)
	c.Assert(prompt2, Equals, prompt1)

	// Duplicate request was not added again to the requests list
	c.Assert(prompt2.Requests(), HasLen, 1)
	c.Check(prompt2.Requests()[0].Key, Equals, "fake:1")

	// Merged prompts should re-record notice
	s.checkNewNoticesSimple(c, []prompting.IDType{prompt1.ID}, nil)
	// Merged prompts should not advance the max ID
	s.checkWrittenMaxID(c, expectedID)
	// Identical requests should not affect mapping
	s.checkWrittenRequestMap(c, expectedMap)

	prompt3, merged, err := pdb.AddOrMerge(metadata, path, permissions, permissions, req1)
	c.Assert(err, IsNil)
	c.Check(merged, Equals, true)
	c.Check(prompt3, Equals, prompt1)

	// Duplicate request was not added again to the requests list
	c.Assert(prompt3.Requests(), HasLen, 1)
	c.Check(prompt3.Requests()[0].Key, Equals, "fake:1")

	// Merged prompts should re-record notice
	s.checkNewNoticesSimple(c, []prompting.IDType{prompt1.ID}, nil)
	// Merged prompts should not advance the max ID
	s.checkWrittenMaxID(c, expectedID)
	// Identical requests should not affect mapping
	s.checkWrittenRequestMap(c, expectedMap)

	stored, err = pdb.Prompts(metadata.User, clientActivity)
	c.Assert(err, IsNil)
	c.Assert(stored, HasLen, 1)
	c.Check(stored[0], Equals, prompt1)
}

func (s *requestpromptsSuite) checkNewNoticesSimple(c *C, expectedPromptIDs []prompting.IDType, expectedData map[string]string) {
	s.checkNewNotices(c, applyNotices(expectedPromptIDs, expectedData))
}

func applyNotices(expectedPromptIDs []prompting.IDType, expectedData map[string]string) []*noticeInfo {
	expectedNotices := make([]*noticeInfo, len(expectedPromptIDs))
	for i, id := range expectedPromptIDs {
		info := &noticeInfo{
			promptID: id,
			data:     expectedData,
		}
		expectedNotices[i] = info
	}
	return expectedNotices
}

func (s *requestpromptsSuite) checkNewNotices(c *C, expectedNotices []*noticeInfo) {
	c.Check(s.promptNotices, DeepEquals, expectedNotices, Commentf("%s", func() string {
		var buf bytes.Buffer
		buf.WriteString("\nobtained: [\n")
		for _, n := range s.promptNotices {
			buf.WriteString(fmt.Sprintf("    %+v\n", n))
		}
		buf.WriteString("]\nexpected: [\n")
		for _, n := range expectedNotices {
			buf.WriteString(fmt.Sprintf("    %+v\n", n))
		}
		buf.WriteString("]\n")
		return buf.String()
	}()))
	s.promptNotices = s.promptNotices[:0]
}

func (s *requestpromptsSuite) checkNewNoticesUnorderedSimple(c *C, expectedPromptIDs []prompting.IDType, expectedData map[string]string) {
	s.checkNewNoticesUnordered(c, applyNotices(expectedPromptIDs, expectedData))
}

func (s *requestpromptsSuite) checkNewNoticesUnordered(c *C, expectedNotices []*noticeInfo) {
	sort.Slice(sortSliceParams(s.promptNotices))
	sort.Slice(sortSliceParams(expectedNotices))
	s.checkNewNotices(c, expectedNotices)
}

func sortSliceParams(list []*noticeInfo) ([]*noticeInfo, func(i, j int) bool) {
	less := func(i, j int) bool {
		return list[i].promptID < list[j].promptID
	}
	return list, less
}

func (s *requestpromptsSuite) TestAddOrMergeTooMany(c *C) {
	pdb, err := requestprompts.New(s.defaultNotifyPrompt)
	c.Assert(err, IsNil)
	defer pdb.Close()

	metadata := &prompting.Metadata{
		User:      s.defaultUser,
		Snap:      "nextcloud",
		PID:       42,
		Cgroup:    "some-cgroup",
		Interface: "home",
	}

	permissions := []string{"read", "write", "execute"}
	clientActivity := false // doesn't matter if it's true or false for this test

	for i := 0; i < requestprompts.MaxOutstandingPromptsPerUser; i++ {
		path := fmt.Sprintf("/home/test/Documents/%d.txt", i)
		request := &prompting.Request{Key: "fake:1"}
		prompt, merged, err := pdb.AddOrMerge(metadata, path, permissions, permissions, request)
		c.Assert(err, IsNil)
		c.Assert(prompt, NotNil)
		c.Assert(merged, Equals, false)
		stored, err := pdb.Prompts(metadata.User, clientActivity)
		c.Assert(err, IsNil)
		c.Assert(stored, HasLen, i+1)
	}

	path := fmt.Sprintf("/home/test/Documents/%d.txt", requestprompts.MaxOutstandingPromptsPerUser)
	req := &prompting.Request{
		Key: "fake:1",
		Reply: func(allowedPermissions []string) error {
			c.Assert(allowedPermissions, HasLen, 0)
			return nil
		},
	}

	// Check that adding a new unmerged prompt fails once limit is reached
	for i := 0; i < 5; i++ {
		prompt, merged, err := pdb.AddOrMerge(metadata, path, permissions, permissions, req)
		c.Check(err, Equals, prompting_errors.ErrTooManyPrompts)
		c.Check(prompt, IsNil)
		c.Check(merged, Equals, false)
		stored, err := pdb.Prompts(metadata.User, clientActivity)
		c.Assert(err, IsNil)
		c.Assert(stored, HasLen, requestprompts.MaxOutstandingPromptsPerUser)
	}

	// Make Reply fail if called
	req.Reply = func(allowedPerms []string) error {
		c.Fatalf("called Reply with %v", allowedPerms)
		return nil
	}

	// Check that new requests can still merge into existing prompts
	for i := 0; i < requestprompts.MaxOutstandingPromptsPerUser; i++ {
		path := fmt.Sprintf("/home/test/Documents/%d.txt", i)
		request := &prompting.Request{Key: "fake:1"}
		prompt, merged, err := pdb.AddOrMerge(metadata, path, permissions, permissions, request)
		c.Assert(err, IsNil)
		c.Assert(prompt, NotNil)
		c.Assert(merged, Equals, true)
		stored, err := pdb.Prompts(metadata.User, clientActivity)
		c.Assert(err, IsNil)
		// Number of stored prompts remains the maximum
		c.Assert(stored, HasLen, requestprompts.MaxOutstandingPromptsPerUser)
	}
}

func (s *requestpromptsSuite) TestPromptWithIDErrors(c *C) {
	pdb, err := requestprompts.New(s.defaultNotifyPrompt)
	c.Assert(err, IsNil)
	defer pdb.Close()

	metadata := &prompting.Metadata{
		User:      s.defaultUser,
		Snap:      "nextcloud",
		PID:       1337,
		Cgroup:    "0::/user.slice/user-1000.slice/user@1000.service/app.slice/foo.scope",
		Interface: "home",
	}
	path := "/home/test/Documents/foo.txt"
	permissions := []string{"read", "write", "execute"}

	request := &prompting.Request{Key: "fake:1"}

	prompt, merged, err := pdb.AddOrMerge(metadata, path, permissions, permissions, request)
	c.Assert(err, IsNil)
	c.Check(merged, Equals, false)

	s.checkNewNoticesSimple(c, []prompting.IDType{prompt.ID}, nil)

	clientActivity := true // doesn't matter if it's true or false for this test
	result, err := pdb.PromptWithID(metadata.User, prompt.ID, clientActivity)
	c.Check(err, IsNil)
	c.Check(result, Equals, prompt)

	result, err = pdb.PromptWithID(metadata.User, 1234, clientActivity)
	c.Check(err, Equals, prompting_errors.ErrPromptNotFound)
	c.Check(result, IsNil)

	result, err = pdb.PromptWithID(metadata.User+1, prompt.ID, clientActivity)
	c.Check(err, Equals, prompting_errors.ErrPromptNotFound)
	c.Check(result, IsNil)

	// Looking up prompts (with or without errors) should not record notices
	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)
}

func (s *requestpromptsSuite) TestReply(c *C) {
	pdb, err := requestprompts.New(s.defaultNotifyPrompt)
	c.Assert(err, IsNil)
	defer pdb.Close()

	metadata := &prompting.Metadata{
		User:      s.defaultUser,
		Snap:      "nextcloud",
		PID:       123,
		Cgroup:    "0::/user.slice/user-1000.slice/user@1000.service/app.slice/foo.scope",
		Interface: "home",
	}
	path := "/home/test/Documents/foo.txt"
	permissions := []string{"read", "write", "execute"}

	promptID := prompting.IDType(0)

	for _, outcome := range []prompting.OutcomeType{prompting.OutcomeAllow, prompting.OutcomeDeny} {
		promptID++
		req1, replyChan1 := newRequestWithReplyChan("fake:1")
		req2, replyChan2 := newRequestWithReplyChan("fake:2")

		prompt1, merged, err := pdb.AddOrMerge(metadata, path, permissions, permissions, req1)
		c.Assert(err, IsNil)
		c.Check(merged, Equals, false)
		c.Assert(prompt1.Requests(), HasLen, 1)
		c.Check(prompt1.Requests()[0].Key, Equals, "fake:1")

		s.checkNewNoticesSimple(c, []prompting.IDType{prompt1.ID}, nil)
		expectedMap := map[string]requestprompts.RequestMapEntry{"fake:1": {PromptID: promptID, UserID: s.defaultUser}}
		s.checkWrittenRequestMap(c, expectedMap)

		prompt2, merged, err := pdb.AddOrMerge(metadata, path, permissions, permissions, req2)
		c.Assert(err, IsNil)
		c.Check(merged, Equals, true)
		c.Check(prompt2, Equals, prompt1)
		c.Assert(prompt2.Requests(), HasLen, 2)
		c.Check(prompt2.Requests()[0].Key, Equals, "fake:1")
		c.Check(prompt2.Requests()[1].Key, Equals, "fake:2")

		// Merged prompts should re-record notice
		s.checkNewNoticesSimple(c, []prompting.IDType{prompt1.ID}, nil)
		expectedMap["fake:2"] = requestprompts.RequestMapEntry{PromptID: promptID, UserID: s.defaultUser}
		s.checkWrittenRequestMap(c, expectedMap)

		// Re-send original request again to make sure we don't get duplicate replies
		prompt3, merged, err := pdb.AddOrMerge(metadata, path, permissions, permissions, req1)
		c.Assert(err, IsNil)
		c.Check(merged, Equals, true)
		c.Check(prompt3, Equals, prompt1)
		c.Assert(prompt3.Requests(), HasLen, 2)
		c.Check(prompt3.Requests()[0].Key, Equals, "fake:1")
		c.Check(prompt3.Requests()[1].Key, Equals, "fake:2")

		// Merged prompts should re-record notice
		s.checkNewNoticesSimple(c, []prompting.IDType{prompt1.ID}, nil)
		// Request is re-received, so ID map should not change
		s.checkWrittenRequestMap(c, expectedMap)

		clientActivity := true // doesn't matter if it's true or false for this test
		repliedPrompt, err := pdb.Reply(metadata.User, prompt1.ID, outcome, clientActivity)
		c.Check(err, IsNil)
		c.Check(repliedPrompt, Equals, prompt1)
		// Make sure we only get one reply per request, even though one request
		// was sent twice
		for _, replyChan := range []<-chan []string{replyChan1, replyChan2} {
			allowedPerms := waitForReply(c, replyChan)
			allow, err := outcome.AsBool()
			c.Check(err, IsNil)
			if allow {
				// Check that permissions in response match prompt's permissions
				c.Check(allowedPerms, DeepEquals, prompt1.Constraints.OutstandingPermissions())
			} else {
				// Check that no permissions were allowed
				c.Check(allowedPerms, HasLen, 0)
			}
		}

		// Check that no more replies were sent because of the duplicate request
		select {
		case repl := <-replyChan1:
			c.Errorf("received unexpected reply: %v", repl)
		case repl := <-replyChan2:
			c.Errorf("received unexpected reply: %v", repl)
		case <-time.NewTimer(10 * time.Millisecond).C:
			// all good
		}

		expectedData := map[string]string{"resolved": "replied"}
		s.checkNewNoticesSimple(c, []prompting.IDType{repliedPrompt.ID}, expectedData)
		// Reply should have cleared mappings for request IDs associated with replied prompt
		expectedMap = map[string]requestprompts.RequestMapEntry{}
		s.checkWrittenRequestMap(c, expectedMap)
	}
}

func (s *requestpromptsSuite) TestReplyTimedOut(c *C) {
	logbuf, restore := logger.MockDebugLogger()
	defer restore()

	pdb, err := requestprompts.New(s.defaultNotifyPrompt)
	c.Assert(err, IsNil)
	defer pdb.Close()

	metadata := &prompting.Metadata{
		User:      s.defaultUser,
		Snap:      "nextcloud",
		PID:       123,
		Cgroup:    "0::/user.slice/user-1000.slice/user@1000.service/app.slice/some-cgroup-id.scope",
		Interface: "home",
	}
	path := "/home/test/Documents/foo.txt"
	permissions := []string{"read", "write", "execute"}
	outcome := prompting.OutcomeAllow

	// call f, then return an error
	replyWithError := func(f func([]string) error) func([]string) error {
		return func(allowedPerms []string) error {
			f(allowedPerms)
			// Return ENOENT, indicating that the notification does not exist.
			// If the prompt exists but the notification does not, then the
			// notification most likely timed out in the kernel.
			return unix.ENOENT
		}
	}

	req1, replyChan1 := newRequestWithReplyChan("fake:1")
	req1.Reply = replyWithError(req1.Reply)
	req2, replyChan2 := newRequestWithReplyChan("fake:2")
	req2.Reply = replyWithError(req2.Reply)

	prompt1, merged, err := pdb.AddOrMerge(metadata, path, permissions, permissions, req1)
	c.Assert(err, IsNil)
	c.Check(merged, Equals, false)

	s.checkNewNoticesSimple(c, []prompting.IDType{prompt1.ID}, nil)

	prompt2, merged, err := pdb.AddOrMerge(metadata, path, permissions, permissions, req2)
	c.Assert(err, IsNil)
	c.Check(merged, Equals, true)
	c.Check(prompt2, Equals, prompt1)

	// Merged prompts should re-record notice
	s.checkNewNoticesSimple(c, []prompting.IDType{prompt1.ID}, nil)

	clientActivity := true // doesn't matter if it's true or false for this test
	repliedPrompt, err := pdb.Reply(metadata.User, prompt1.ID, outcome, clientActivity)
	c.Check(err, IsNil)
	c.Check(repliedPrompt, Equals, prompt1)
	for _, replyChan := range []<-chan []string{replyChan1, replyChan2} {
		allowedPerms := waitForReply(c, replyChan)
		// Check that permissions in response matches the prompt's permissions
		c.Check(allowedPerms, DeepEquals, prompt1.Constraints.OutstandingPermissions())
	}

	expectedData := map[string]string{"resolved": "replied"}
	s.checkNewNoticesSimple(c, []prompting.IDType{repliedPrompt.ID}, expectedData)

	// Expect two messages containing the following:
	logMsg := "kernel returned ENOENT from APPARMOR_NOTIF_SEND"
	c.Check(logbuf.String(), testutil.MatchesWrapped, fmt.Sprintf(".*%s.*%s.*", logMsg, logMsg))
}

func (s *requestpromptsSuite) TestReplyErrors(c *C) {
	pdb, err := requestprompts.New(s.defaultNotifyPrompt)
	c.Assert(err, IsNil)
	defer pdb.Close()

	metadata := &prompting.Metadata{
		User:      s.defaultUser,
		Snap:      "nextcloud",
		PID:       123,
		Cgroup:    "some-cgroup-path",
		Interface: "home",
	}
	path := "/home/test/Documents/foo.txt"
	permissions := []string{"read", "write", "execute"}

	fakeError := fmt.Errorf("fake reply error")

	req := &prompting.Request{
		Key: "fake:123",
		Reply: func(allowedPerms []string) error {
			return fakeError
		},
	}

	prompt, merged, err := pdb.AddOrMerge(metadata, path, permissions, permissions, req)
	c.Assert(err, IsNil)
	c.Check(merged, Equals, false)

	s.checkNewNoticesSimple(c, []prompting.IDType{prompt.ID}, nil)
	expectedMap := map[string]requestprompts.RequestMapEntry{"fake:123": {PromptID: 1, UserID: s.defaultUser}}
	s.checkWrittenRequestMap(c, expectedMap)

	outcome := prompting.OutcomeAllow

	clientActivity := true // doesn't matter if it's true or false for this test
	_, err = pdb.Reply(metadata.User, 1234, outcome, clientActivity)
	c.Check(err, Equals, prompting_errors.ErrPromptNotFound)

	_, err = pdb.Reply(metadata.User+1, prompt.ID, outcome, clientActivity)
	c.Check(err, Equals, prompting_errors.ErrPromptNotFound)

	_, err = pdb.Reply(metadata.User, prompt.ID, outcome, clientActivity)
	c.Check(err, Equals, fakeError)

	// Failed replies should not record notice
	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)
	// Failed replies should not affect ID map
	s.checkWrittenRequestMap(c, expectedMap)
}

func (s *requestpromptsSuite) TestHandleNewRule(c *C) {
	pdb, err := requestprompts.New(s.defaultNotifyPrompt)
	c.Assert(err, IsNil)
	defer pdb.Close()

	metadata := &prompting.Metadata{
		User:      s.defaultUser,
		Snap:      "nextcloud",
		PID:       123,
		Cgroup:    "some-cgroup-path",
		Interface: "home",
	}
	path := "/home/test/Documents/foo.txt"

	permissions1 := []string{"read", "write", "execute"}
	req1, replyChan1 := newRequestWithReplyChan("fake:12")
	prompt1, merged, err := pdb.AddOrMerge(metadata, path, permissions1, permissions1, req1)
	c.Assert(err, IsNil)
	c.Check(merged, Equals, false)

	permissions2 := []string{"read", "write"}
	req2, replyChan2 := newRequestWithReplyChan("fake:34")
	prompt2, merged, err := pdb.AddOrMerge(metadata, path, permissions2, permissions2, req2)
	c.Assert(err, IsNil)
	c.Check(merged, Equals, false)

	permissions3 := []string{"read"}
	req3, replyChan3 := newRequestWithReplyChan("fake:56")
	prompt3, merged, err := pdb.AddOrMerge(metadata, path, permissions3, permissions3, req3)
	c.Assert(err, IsNil)
	c.Check(merged, Equals, false)

	permissions4 := []string{"open"}
	req4 := &prompting.Request{Key: "fake:78"}
	prompt4, merged, err := pdb.AddOrMerge(metadata, path, permissions4, permissions4, req4)
	c.Assert(err, IsNil)
	c.Check(merged, Equals, false)

	s.checkNewNoticesSimple(c, []prompting.IDType{prompt1.ID, prompt2.ID, prompt3.ID, prompt4.ID}, nil)
	expectedMap := map[string]requestprompts.RequestMapEntry{
		"fake:12": {PromptID: 1, UserID: s.defaultUser},
		"fake:34": {PromptID: 2, UserID: s.defaultUser},
		"fake:56": {PromptID: 3, UserID: s.defaultUser},
		"fake:78": {PromptID: 4, UserID: s.defaultUser},
	}
	s.checkWrittenRequestMap(c, expectedMap)

	clientActivity := false // doesn't matter if it's true or false for this test
	stored, err := pdb.Prompts(metadata.User, clientActivity)
	c.Assert(err, IsNil)
	c.Assert(stored, HasLen, 4)

	pathPattern, err := patterns.ParsePathPattern("/home/test/Documents/**")
	c.Assert(err, IsNil)
	constraints := &prompting.RuleConstraints{
		InterfaceSpecific: &prompting.InterfaceSpecificConstraintsHome{
			Pattern: pathPattern,
		},
		Permissions: prompting.RulePermissionMap{
			"read":    &prompting.RulePermissionEntry{Outcome: prompting.OutcomeAllow},
			"execute": &prompting.RulePermissionEntry{Outcome: prompting.OutcomeDeny},
			"append":  &prompting.RulePermissionEntry{Outcome: prompting.OutcomeAllow},
		},
	}

	// For completeness, set PID and Cgroup to empty since they would not be populated for rules
	metadata.PID = 0
	metadata.Cgroup = ""

	satisfied, err := pdb.HandleNewRule(metadata, constraints)
	c.Assert(err, IsNil)
	c.Check(satisfied, HasLen, 2)
	c.Check(promptIDListContains(satisfied, prompt1.ID), Equals, true)
	c.Check(promptIDListContains(satisfied, prompt3.ID), Equals, true)

	// Read permissions of prompt2 satisfied, but it has one outstanding
	// permission, so notice re-issued. prompt1 satisfied because at least
	// one permission was denied, and prompt3 permissions fully satisfied.
	e1 := &noticeInfo{promptID: prompt1.ID, data: map[string]string{"resolved": "satisfied"}}
	e2 := &noticeInfo{promptID: prompt2.ID, data: nil}
	e3 := &noticeInfo{promptID: prompt3.ID, data: map[string]string{"resolved": "satisfied"}}
	expectedNotices := []*noticeInfo{e1, e2, e3}
	s.checkNewNoticesUnordered(c, expectedNotices)
	// Check that mappings cleaned up for requests associated with resolved prompts
	delete(expectedMap, "fake:12")
	delete(expectedMap, "fake:56")
	s.checkWrittenRequestMap(c, expectedMap)

	reply1, reply3 := waitForReplies(c, replyChan1, replyChan3)
	// Only "read" permission was allowed for either prompt.
	// prompt1 had requested "write" and "execute" as well, but because
	// "execute" was denied and there was no rule pertaining to "write",
	// the latter were both denied, leaving "read" as the only permission
	// allowed.
	expectedPerms := []string{"read"}
	c.Check(reply1, DeepEquals, expectedPerms)
	c.Check(reply3, DeepEquals, expectedPerms)

	stored, err = pdb.Prompts(metadata.User, clientActivity)
	c.Assert(err, IsNil)
	c.Assert(stored, HasLen, 2)

	// Check that allowing the final missing permission of prompt2 satisfies it
	// with an allow response.
	constraints = &prompting.RuleConstraints{
		InterfaceSpecific: &prompting.InterfaceSpecificConstraintsHome{
			Pattern: pathPattern,
		},
		Permissions: prompting.RulePermissionMap{
			"write": &prompting.RulePermissionEntry{Outcome: prompting.OutcomeAllow},
		},
	}
	satisfied, err = pdb.HandleNewRule(metadata, constraints)

	c.Assert(err, IsNil)
	c.Check(satisfied, HasLen, 1)
	c.Check(satisfied[0], Equals, prompt2.ID)

	expectedData := map[string]string{"resolved": "satisfied"}
	s.checkNewNoticesSimple(c, []prompting.IDType{prompt2.ID}, expectedData)
	// Check that mappings cleaned up for request associated with resolved prompt
	delete(expectedMap, "fake:34")
	s.checkWrittenRequestMap(c, expectedMap)

	reply2 := waitForReply(c, replyChan2)
	c.Check(reply2, DeepEquals, permissions2)
}

func promptIDListContains(haystack []prompting.IDType, needle prompting.IDType) bool {
	for _, id := range haystack {
		if id == needle {
			return true
		}
	}
	return false
}

func (s *requestpromptsSuite) TestHandleNewRuleNonMatches(c *C) {
	pdb, err := requestprompts.New(s.defaultNotifyPrompt)
	c.Assert(err, IsNil)
	defer pdb.Close()

	user := s.defaultUser
	snap := "nextcloud"
	iface := "home"
	metadata := &prompting.Metadata{
		User:      user,
		Snap:      snap,
		PID:       123,
		Cgroup:    "0::/user.slice/user-1000.slice/user@1000.service/app.slice/some-cgroup.scope",
		Interface: iface,
	}
	path := "/home/test/Documents/foo.txt"
	permissions := []string{"read"}
	req, replyChan := newRequestWithReplyChan("fake:1")
	prompt, merged, err := pdb.AddOrMerge(metadata, path, permissions, permissions, req)
	c.Assert(err, IsNil)
	c.Check(merged, Equals, false)

	s.checkNewNoticesSimple(c, []prompting.IDType{prompt.ID}, nil)

	// For completeness, set PID and Cgroup to empty since they would not be populated for rules
	metadata.PID = 0
	metadata.Cgroup = ""

	pathPattern, err := patterns.ParsePathPattern("/home/test/Documents/**")
	c.Assert(err, IsNil)
	constraints := &prompting.RuleConstraints{
		InterfaceSpecific: &prompting.InterfaceSpecificConstraintsHome{
			Pattern: pathPattern,
		},
		Permissions: prompting.RulePermissionMap{
			"read": &prompting.RulePermissionEntry{Outcome: prompting.OutcomeAllow},
		},
	}

	badOutcomeConstraints := &prompting.RuleConstraints{
		InterfaceSpecific: &prompting.InterfaceSpecificConstraintsHome{
			Pattern: pathPattern,
		},
		Permissions: prompting.RulePermissionMap{
			"read": &prompting.RulePermissionEntry{Outcome: prompting.OutcomeType("foo")},
		},
	}

	otherUser := user + 1
	otherSnap := "ldx"
	otherInterface := "system-files"
	otherPattern, err := patterns.ParsePathPattern("/home/test/Pictures/**.png")
	c.Assert(err, IsNil)
	otherConstraints := &prompting.RuleConstraints{
		InterfaceSpecific: &prompting.InterfaceSpecificConstraintsHome{
			Pattern: otherPattern,
		},
		Permissions: prompting.RulePermissionMap{
			"read": &prompting.RulePermissionEntry{Outcome: prompting.OutcomeAllow},
		},
	}

	clientActivity := false // doesn't matter if it's true or false for this test
	stored, err := pdb.Prompts(metadata.User, clientActivity)
	c.Assert(err, IsNil)
	c.Assert(stored, HasLen, 1)
	c.Assert(stored[0], Equals, prompt)

	satisfied, err := pdb.HandleNewRule(metadata, badOutcomeConstraints)
	c.Check(err, ErrorMatches, `invalid outcome: "foo"`)
	c.Check(satisfied, IsNil)

	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)

	otherUserMetadata := &prompting.Metadata{
		User:      otherUser,
		Snap:      snap,
		Interface: iface,
	}
	satisfied, err = pdb.HandleNewRule(otherUserMetadata, constraints)
	c.Check(err, IsNil)
	c.Check(satisfied, IsNil)

	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)

	otherSnapMetadata := &prompting.Metadata{
		User:      user,
		Snap:      otherSnap,
		Interface: iface,
	}
	satisfied, err = pdb.HandleNewRule(otherSnapMetadata, constraints)
	c.Check(err, IsNil)
	c.Check(satisfied, IsNil)

	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)

	otherInterfaceMetadata := &prompting.Metadata{
		User:      user,
		Snap:      snap,
		Interface: otherInterface,
	}
	satisfied, err = pdb.HandleNewRule(otherInterfaceMetadata, constraints)
	c.Check(err, IsNil)
	c.Check(satisfied, IsNil)

	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)

	satisfied, err = pdb.HandleNewRule(metadata, otherConstraints)
	c.Check(err, IsNil)
	c.Check(satisfied, IsNil)

	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)

	satisfied, err = pdb.HandleNewRule(metadata, constraints)
	c.Check(err, IsNil)
	c.Assert(satisfied, HasLen, 1)

	expectedData := map[string]string{"resolved": "satisfied"}
	s.checkNewNoticesSimple(c, []prompting.IDType{prompt.ID}, expectedData)

	allowedPerms := waitForReply(c, replyChan)
	c.Check(allowedPerms, DeepEquals, permissions)

	stored, err = pdb.Prompts(metadata.User, clientActivity)
	c.Check(err, IsNil)
	c.Check(stored, IsNil)
}

func (s *requestpromptsSuite) TestClose(c *C) {
	var timer *testtime.TestTimer
	restore := requestprompts.MockTimeAfterFunc(func(d time.Duration, f func()) timeutil.Timer {
		if timer != nil {
			c.Fatalf("created more than one timer")
		}
		timer = testtime.AfterFunc(d, f)
		return timer
	})
	defer restore()

	pdb, err := requestprompts.New(s.defaultNotifyPrompt)
	c.Assert(err, IsNil)

	metadata := &prompting.Metadata{
		User:      s.defaultUser,
		Snap:      "nextcloud",
		PID:       1234,
		Cgroup:    "0::/user.slice/user-1000.slice/user@1000.service/app.slice/some-cgroup.scope",
		Interface: "home",
	}
	permissions := []string{"read", "write", "execute"}

	paths := []string{
		"/home/test/1.txt",
		"/home/test/2.txt",
		"/home/test/3.txt",
	}

	prompts := make([]*requestprompts.Prompt, 0, 3)
	for i, path := range paths {
		req := &prompting.Request{Key: fmt.Sprintf("fake:%d", i)}
		prompt, merged, err := pdb.AddOrMerge(metadata, path, permissions, permissions, req)
		c.Assert(err, IsNil)
		c.Assert(merged, Equals, false)
		prompts = append(prompts, prompt)
	}

	expectedPromptIDs := make([]prompting.IDType, 0, 3)
	for _, prompt := range prompts {
		expectedPromptIDs = append(expectedPromptIDs, prompt.ID)
	}
	c.Check(prompts[2].ID, Equals, prompting.IDType(3))

	expectedMap := map[string]requestprompts.RequestMapEntry{
		"fake:0": {PromptID: 1, UserID: s.defaultUser},
		"fake:1": {PromptID: 2, UserID: s.defaultUser},
		"fake:2": {PromptID: 3, UserID: s.defaultUser},
	}
	s.checkWrittenRequestMap(c, expectedMap)

	// One notice for each prompt when created
	s.checkNewNoticesSimple(c, expectedPromptIDs, nil)

	pdb.Close()

	// No notices should be recorded when snapd is restarting, so that we can
	// pick back up where we left off
	s.checkNewNoticesUnorderedSimple(c, nil, nil)

	// ID map still on disk
	s.checkWrittenRequestMap(c, expectedMap)

	// All prompts have been cleared, and all per-user maps deleted
	c.Check(pdb.PerUser(), HasLen, 0)

	// Sense check that the timer is still active, though this is not part of
	// any contract, and there's no reason that closing the timer shouldn't be
	// allowed to stop the expiration timers. We don't at the moment because
	// doing so is racy and unnecessary, though there's no harm in closing them.
	c.Check(timer.Active(), Equals, true)

	// Elapse time as if the prompt timer expired
	timer.Elapse(requestprompts.InitialTimeout + requestprompts.ActivityTimeout)
	// Check that timer expiration did not result in new notices
	s.checkNewNoticesSimple(c, []prompting.IDType{}, nil)
	// Check that the timer is no longer active. Since the DB is closed, the
	// expiration callback should not reset the timer as it usually would.
	c.Check(timer.Active(), Equals, false)
}

func (s *requestpromptsSuite) TestCloseThenOperate(c *C) {
	pdb, err := requestprompts.New(s.defaultNotifyPrompt)
	c.Assert(err, IsNil)

	err = pdb.Close()
	c.Assert(err, IsNil)

	nextID, err := pdb.NextID()
	c.Check(err, Equals, maxidmmap.ErrMaxIDMmapClosed)
	c.Check(nextID, Equals, prompting.IDType(0))

	metadata := prompting.Metadata{Interface: "home"}
	result, merged, err := pdb.AddOrMerge(&metadata, "", nil, nil, nil)
	c.Check(err, Equals, prompting_errors.ErrPromptsClosed)
	c.Check(result, IsNil)
	c.Check(merged, Equals, false)

	clientActivity := false // doesn't matter if it's true or false for this test
	prompts, err := pdb.Prompts(1000, clientActivity)
	c.Check(err, Equals, prompting_errors.ErrPromptsClosed)
	c.Check(prompts, IsNil)

	prompt, err := pdb.PromptWithID(1000, 1, clientActivity)
	c.Check(err, Equals, prompting_errors.ErrPromptsClosed)
	c.Check(prompt, IsNil)

	result, err = pdb.Reply(1000, 1, prompting.OutcomeDeny, clientActivity)
	c.Check(err, Equals, prompting_errors.ErrPromptsClosed)
	c.Check(result, IsNil)

	promptIDs, err := pdb.HandleNewRule(nil, nil)
	c.Check(err, Equals, prompting_errors.ErrPromptsClosed)
	c.Check(promptIDs, IsNil)

	err = pdb.Close()
	c.Check(err, Equals, prompting_errors.ErrPromptsClosed)
}

func (s *requestpromptsSuite) TestIDMappingAcrossRestarts(c *C) {
	req1 := &prompting.Request{Key: "fake:1"}
	req2 := &prompting.Request{Key: "fake:2"}
	req3 := &prompting.Request{Key: "fake:3"}
	req4 := &prompting.Request{Key: "fake:5"}
	req5 := &prompting.Request{Key: "fake:8"}

	// Write initial mappings from request IDs to prompt IDs
	c.Assert(os.MkdirAll(dirs.SnapInterfacesRequestsRunDir, 0o777), IsNil)
	mapping := requestprompts.RequestMappingJSON{
		RequestMap: map[string]requestprompts.RequestMapEntry{
			"fake:1": {PromptID: 1, UserID: s.defaultUser},
			"fake:2": {PromptID: 2, UserID: s.defaultUser},
			"fake:3": {PromptID: 1, UserID: s.defaultUser}, // third request merged with first request
		},
	}
	data, err := json.Marshal(mapping)
	c.Assert(err, IsNil)
	c.Assert(osutil.AtomicWriteFile(s.requestIDMapFilepath, data, 0o600, 0), IsNil)
	// Write max ID corresponding to mapping
	var maxIDData [8]byte
	*(*uint64)(unsafe.Pointer(&maxIDData)) = uint64(2)
	c.Assert(osutil.AtomicWriteFile(s.maxIDPath, maxIDData[:], 0o600, 0), IsNil)

	pdb, err := requestprompts.New(s.defaultNotifyPrompt)
	c.Assert(err, IsNil)
	defer pdb.Close()

	metadataTemplate := prompting.Metadata{
		User:      s.defaultUser,
		Snap:      "nextcloud",
		PID:       1234,
		Cgroup:    "0::/user.slice/user-1000.slice/user@1000.service/app.slice/some-cgroup.scope",
		Interface: "home",
	}
	path := "/home/test/Documents/foo.txt"
	permissions := []string{"read", "write", "execute"}

	clientActivity := false // doesn't matter if it's true or false for this test

	metadata := metadataTemplate
	prompt1, merged, err := pdb.AddOrMerge(&metadata, path, permissions, permissions, req1)
	c.Assert(err, IsNil)
	c.Check(merged, Equals, false)
	c.Check(prompt1.ID, Equals, prompting.IDType(1))

	// Add second request, this time with different snap
	metadata = metadataTemplate
	metadata.Snap = "firefox"
	prompt2, merged, err := pdb.AddOrMerge(&metadata, path, permissions, permissions, req2)
	c.Assert(err, IsNil)
	c.Check(merged, Equals, false)
	c.Check(prompt2, Not(Equals), prompt1)
	c.Check(prompt2.ID, Equals, prompting.IDType(2))

	// Add third request, this time identical to the first
	metadata = metadataTemplate
	prompt3, merged, err := pdb.AddOrMerge(&metadata, path, permissions, permissions, req3)
	c.Assert(err, IsNil)
	c.Check(merged, Equals, true)
	c.Check(prompt3, Equals, prompt1)
	c.Check(prompt3, Not(Equals), prompt2)
	c.Check(prompt3.ID, Equals, prompting.IDType(1))

	// Add fourth request, this time with different PID but same Cgroup
	metadata = metadataTemplate
	metadata.PID = 5000
	prompt4, merged, err := pdb.AddOrMerge(&metadata, path, permissions, permissions, req4)
	c.Assert(err, IsNil)
	c.Check(merged, Equals, false)
	c.Check(prompt4, Not(Equals), prompt1)
	c.Check(prompt4, Not(Equals), prompt2)
	c.Check(prompt4.ID, Equals, prompting.IDType(3))

	// Add fifth request, this time identical to prompt2
	metadata = metadataTemplate
	metadata.Snap = "firefox"
	prompt5, merged, err := pdb.AddOrMerge(&metadata, path, permissions, permissions, req5)
	c.Assert(err, IsNil)
	c.Check(merged, Equals, true)
	c.Check(prompt5, Equals, prompt2)
	c.Check(prompt5.ID, Equals, prompting.IDType(2))

	// New prompts should record notice and merged prompt should re-notify
	s.checkNewNoticesSimple(c, []prompting.IDType{prompt1.ID, prompt2.ID, prompt1.ID, prompt4.ID, prompt2.ID}, nil)
	// New prompts should advance the max ID
	expectedID := uint64(3)
	expectedMap := map[string]requestprompts.RequestMapEntry{
		"fake:1": {PromptID: 1, UserID: s.defaultUser}, // originally mapped
		"fake:2": {PromptID: 2, UserID: s.defaultUser}, // originally mapped
		"fake:3": {PromptID: 1, UserID: s.defaultUser}, // originally mapped
		"fake:5": {PromptID: 3, UserID: s.defaultUser}, // new, new prompt
		"fake:8": {PromptID: 2, UserID: s.defaultUser}, // new, merged with prompt2
	}
	s.checkWrittenMaxID(c, expectedID)
	s.checkWrittenRequestMap(c, expectedMap)

	stored, err := pdb.Prompts(metadata.User, clientActivity)
	c.Assert(err, IsNil)
	c.Assert(stored, HasLen, 3)
	c.Check(stored[0], Equals, prompt1)
	c.Check(stored[1], Equals, prompt2)
	c.Check(stored[2], Equals, prompt4)
}

func (s *requestpromptsSuite) TestHandleReadying(c *C) {
	req1 := &prompting.Request{Key: "fake:1"}
	req2 := &prompting.Request{Key: "fake:2"}

	// Write initial mappings from request IDs to prompt IDs
	c.Assert(os.MkdirAll(dirs.SnapInterfacesRequestsRunDir, 0o777), IsNil)
	mapping := requestprompts.RequestMappingJSON{
		RequestMap: map[string]requestprompts.RequestMapEntry{
			"fake:1": {PromptID: 1, UserID: s.defaultUser},
			"fake:2": {PromptID: 2, UserID: s.defaultUser},
			"fake:3": {PromptID: 1, UserID: s.defaultUser}, // third request merged with first request
			"fake:4": {PromptID: 3, UserID: s.defaultUser}, // we won't re-receive request 4
		},
	}
	data, err := json.Marshal(mapping)
	c.Assert(err, IsNil)
	c.Assert(osutil.AtomicWriteFile(s.requestIDMapFilepath, data, 0o600, 0), IsNil)
	// Write max ID corresponding to mapping
	expectedID := uint64(3)
	var maxIDData [8]byte
	*(*uint64)(unsafe.Pointer(&maxIDData)) = expectedID
	c.Assert(osutil.AtomicWriteFile(s.maxIDPath, maxIDData[:], 0o600, 0), IsNil)

	pdb, err := requestprompts.New(s.defaultNotifyPrompt)
	c.Assert(err, IsNil)
	defer pdb.Close()

	metadataTemplate := prompting.Metadata{
		User:      s.defaultUser,
		Snap:      "nextcloud",
		PID:       1234,
		Cgroup:    "0::/user.slice/user-1000.slice/user@1000.service/app.slice/some-cgroup.scope",
		Interface: "home",
	}
	path := "/home/test/Documents/foo.txt"
	permissions := []string{"read", "write", "execute"}

	clientActivity := false // doesn't matter if it's true or false for this test

	// Receive first request
	metadata := metadataTemplate
	prompt1, merged, err := pdb.AddOrMerge(&metadata, path, permissions, permissions, req1)
	c.Assert(err, IsNil)
	c.Check(merged, Equals, false)
	c.Check(prompt1.ID, Equals, prompting.IDType(1))

	// Receive second request, this time with different snap
	metadata = metadataTemplate
	metadata.Snap = "firefox"
	prompt2, merged, err := pdb.AddOrMerge(&metadata, path, permissions, permissions, req2)
	c.Assert(err, IsNil)
	c.Check(merged, Equals, false)
	c.Check(prompt2, Not(Equals), prompt1)
	c.Check(prompt2.ID, Equals, prompting.IDType(2))

	// Do *not* re-receive third or fourth request

	// New prompts should record notices
	s.checkNewNoticesSimple(c, []prompting.IDType{prompt1.ID, prompt2.ID}, nil)
	// All received prompts were previously sent, so expected map and max ID
	// should be unchanged.
	expectedMap := map[string]requestprompts.RequestMapEntry{
		"fake:1": {PromptID: 1, UserID: s.defaultUser},
		"fake:2": {PromptID: 2, UserID: s.defaultUser},
		"fake:3": {PromptID: 1, UserID: s.defaultUser},
		"fake:4": {PromptID: 3, UserID: s.defaultUser},
	}
	s.checkWrittenMaxID(c, expectedID)
	s.checkWrittenRequestMap(c, expectedMap)

	stored, err := pdb.Prompts(metadata.User, clientActivity)
	c.Assert(err, IsNil)
	c.Assert(stored, HasLen, 2)
	c.Check(stored[0], Equals, prompt1)
	c.Check(stored[1], Equals, prompt2)

	// Signal that the manager is readying
	err = pdb.HandleReadying()
	c.Check(err, IsNil)

	// Notice should be recorded for every prompt for which no notice was
	// re-received. That is, prompt ID 3.
	noticeData := map[string]string{"resolved": "expired"}
	s.checkNewNoticesSimple(c, []prompting.IDType{3}, noticeData)
	// Handling ready should prune all pending requests which have not been
	// re-received.
	expectedMap = map[string]requestprompts.RequestMapEntry{
		"fake:1": {PromptID: 1, UserID: s.defaultUser},
		"fake:2": {PromptID: 2, UserID: s.defaultUser},
	}
	s.checkWrittenMaxID(c, expectedID)
	s.checkWrittenRequestMap(c, expectedMap)
}

func (s *requestpromptsSuite) TestPromptMarshalJSON(c *C) {
	pdb, err := requestprompts.New(s.defaultNotifyPrompt)
	c.Assert(err, IsNil)
	defer pdb.Close()

	timestampStr := "2024-08-14T09:47:03.350324989-05:00"
	timestamp, err := time.Parse(time.RFC3339Nano, timestampStr)
	c.Assert(err, IsNil)

	for _, testCase := range []struct {
		metadata         *prompting.Metadata
		path             string
		requestedPerms   []string
		outstandingPerms []string
		expected         string
	}{
		{
			metadata: &prompting.Metadata{
				User:      s.defaultUser,
				Snap:      "firefox",
				PID:       1234,
				Cgroup:    "0::/user.slice/user-1000.slice/user@1000.service/app.slice/some-cgroup.scope",
				Interface: "home",
			},
			path:             "/home/test/foo",
			requestedPerms:   []string{"read", "write", "execute"},
			outstandingPerms: []string{"write", "execute"},
			expected:         `{"id":"0000000000000001","timestamp":"2024-08-14T09:47:03.350324989-05:00","snap":"firefox","pid":1234,"cgroup":"0::/user.slice/user-1000.slice/user@1000.service/app.slice/some-cgroup.scope","interface":"home","constraints":{"path":"/home/test/foo","requested-permissions":["write","execute"],"available-permissions":["read","write","execute"]}}`,
		},
		{
			metadata: &prompting.Metadata{
				User:      s.defaultUser,
				Snap:      "thunderbird",
				PID:       112358,
				Cgroup:    "0::/user.slice/user-1000.slice/user@1000.service/app.slice/some-cgroup.scope",
				Interface: "camera",
			},
			path:             "/dev/video0",
			requestedPerms:   []string{"access"},
			outstandingPerms: []string{"access"},
			expected:         `{"id":"0000000000000002","timestamp":"2024-08-14T09:47:03.350324989-05:00","snap":"thunderbird","pid":112358,"cgroup":"0::/user.slice/user-1000.slice/user@1000.service/app.slice/some-cgroup.scope","interface":"camera","constraints":{"requested-permissions":["access"],"available-permissions":["access"]}}`,
		},
	} {
		fakeRequest := &prompting.Request{Key: "fake:1234"}

		prompt, merged, err := pdb.AddOrMerge(testCase.metadata, testCase.path, testCase.requestedPerms, testCase.outstandingPerms, fakeRequest)
		c.Assert(err, IsNil)
		c.Check(merged, Equals, false)

		// Set timestamp to known time
		prompt.Timestamp = timestamp
		c.Assert(err, IsNil)

		marshalled, err := json.Marshal(prompt)
		c.Assert(err, IsNil)

		c.Check(string(marshalled), Equals, testCase.expected)
	}
}

func (s *requestpromptsSuite) TestPromptExpiration(c *C) {
	var timer *testtime.TestTimer
	restore := requestprompts.MockTimeAfterFunc(func(d time.Duration, f func()) timeutil.Timer {
		if timer != nil {
			c.Fatalf("created more than one timer")
		}
		timer = testtime.AfterFunc(d, f)
		return timer
	})
	defer restore()

	metadata := &prompting.Metadata{
		User:      s.defaultUser,
		Snap:      "firefox",
		PID:       1234,
		Cgroup:    "0::/user.slice/user-1000.slice/user@1000.service/app.slice/some-cgroup.scope",
		Interface: "home",
	}
	path := "/home/test/foo"
	requestedPermissions := []string{"read", "write", "execute"}
	outstandingPermissions := []string{"write", "execute"}

	noticeChan := make(chan noticeInfo, 1)
	pdb, err := requestprompts.New(func(userID uint32, promptID prompting.IDType, data map[string]string) error {
		c.Assert(userID, Equals, s.defaultUser)
		noticeChan <- noticeInfo{
			promptID: promptID,
			data:     data,
		}
		return nil
	})
	c.Assert(err, IsNil)
	defer pdb.Close()

	// Add prompt
	req1, replyChan1 := newRequestWithReplyChan("fake:123")
	prompt, merged, err := pdb.AddOrMerge(metadata, path, requestedPermissions, outstandingPermissions, req1)
	c.Assert(err, IsNil)
	c.Assert(merged, Equals, false)
	checkCurrentNotices(c, noticeChan, prompt.ID, nil)
	expectedMap := map[string]requestprompts.RequestMapEntry{"fake:123": {PromptID: 1, UserID: s.defaultUser}}
	s.checkWrittenRequestMap(c, expectedMap)

	// Check that prompt has not immediately expired
	c.Assert(timer.FireCount(), Equals, 0)

	// Prompt should *not* expire after half of initialTimeout
	timer.Elapse(requestprompts.InitialTimeout / 2)
	c.Assert(timer.FireCount(), Equals, 0)

	// Add another prompt, check that it does not bump the activity timeout
	req2, replyChan2 := newRequestWithReplyChan("fake:456")
	otherPath := "/home/test/bar"
	prompt2, merged, err := pdb.AddOrMerge(metadata, otherPath, requestedPermissions, outstandingPermissions, req2)
	c.Assert(err, IsNil)
	c.Assert(merged, Equals, false)
	checkCurrentNotices(c, noticeChan, prompt2.ID, nil)
	expectedMap["fake:456"] = requestprompts.RequestMapEntry{PromptID: 2, UserID: s.defaultUser}
	s.checkWrittenRequestMap(c, expectedMap)

	// Prompt should expire after initialTimeout, but half already elapsed
	timer.Elapse(requestprompts.InitialTimeout - requestprompts.InitialTimeout/2)
	checkCurrentNoticesMultiple(c, noticeChan, []prompting.IDType{prompt.ID, prompt2.ID}, map[string]string{"resolved": "expired"})
	// Expect two replies, one for each prompt
	allowedPerms := waitForReply(c, replyChan1)
	c.Check(allowedPerms, DeepEquals, []string{"read"})
	allowedPerms = waitForReply(c, replyChan2)
	c.Check(allowedPerms, DeepEquals, []string{"read"})
	c.Assert(timer.FireCount(), Equals, 1)
	// ID mappings should have been cleaned up for requests associated with expired prompt
	expectedMap = map[string]requestprompts.RequestMapEntry{}
	s.checkWrittenRequestMap(c, expectedMap)

	// Add prompt again
	req3, replyChan3 := newRequestWithReplyChan("fake:789")
	prompt, merged, err = pdb.AddOrMerge(metadata, path, requestedPermissions, outstandingPermissions, req3)
	c.Assert(err, IsNil)
	c.Assert(merged, Equals, false)
	checkCurrentNotices(c, noticeChan, prompt.ID, nil)
	expectedMap["fake:789"] = requestprompts.RequestMapEntry{PromptID: 3, UserID: s.defaultUser}
	s.checkWrittenRequestMap(c, expectedMap)

	// Retrieve prompts for s.defaultUser, and bump timeout
	clientActivity := true
	prompts, err := pdb.Prompts(s.defaultUser, clientActivity)
	c.Check(err, IsNil)
	c.Check(prompts, DeepEquals, []*requestprompts.Prompt{prompt})

	// Prompt should *not* expire after initialTimeout (or even double it)
	timer.Elapse(2 * requestprompts.InitialTimeout)
	c.Assert(timer.FireCount(), Equals, 1)

	// Retrieve prompt by ID, and bump timeout
	p, err := pdb.PromptWithID(s.defaultUser, prompt.ID, clientActivity)
	c.Check(err, IsNil)
	c.Check(p, Equals, prompt)

	// Prompt should *not* expire after activityTimeout-1ns
	timer.Elapse(requestprompts.ActivityTimeout - time.Nanosecond)
	c.Assert(timer.FireCount(), Equals, 1)

	// Reply to fake prompt (and get error, but still bump timeout)
	_, err = pdb.Reply(s.defaultUser, prompt.ID+1, prompting.OutcomeAllow, clientActivity)
	c.Check(err, NotNil)

	// Prompt should *not* expire after initialTimeout
	timer.Elapse(requestprompts.InitialTimeout)
	c.Assert(timer.FireCount(), Equals, 1)

	s.checkWrittenRequestMap(c, expectedMap)

	// Prompt should expire after activityTimeout
	timer.Elapse(requestprompts.ActivityTimeout - requestprompts.InitialTimeout)
	checkCurrentNotices(c, noticeChan, prompt.ID, map[string]string{"resolved": "expired"})
	allowedPerms = waitForReply(c, replyChan3)
	c.Check(allowedPerms, DeepEquals, []string{"read"})
	c.Assert(timer.FireCount(), Equals, 2)

	expectedMap = map[string]requestprompts.RequestMapEntry{}
	s.checkWrittenRequestMap(c, expectedMap)

	// Add prompt again
	req4, replyChan4 := newRequestWithReplyChan("fake:101112")
	prompt, merged, err = pdb.AddOrMerge(metadata, path, requestedPermissions, outstandingPermissions, req4)
	c.Assert(err, IsNil)
	c.Assert(merged, Equals, false)
	checkCurrentNotices(c, noticeChan, prompt.ID, nil)
	expectedMap["fake:101112"] = requestprompts.RequestMapEntry{PromptID: 4, UserID: s.defaultUser}
	s.checkWrittenRequestMap(c, expectedMap)

	// Check that prompt has not immediately expired
	c.Assert(timer.FireCount(), Equals, 2)
	// Nor after initialTimeout-1ns
	timer.Elapse(requestprompts.InitialTimeout - time.Nanosecond)
	c.Assert(timer.FireCount(), Equals, 2)

	// Get prompts but do not bump timeout
	clientActivity = false
	prompts, err = pdb.Prompts(s.defaultUser, clientActivity)
	c.Check(err, IsNil)
	c.Check(prompts, DeepEquals, []*requestprompts.Prompt{prompt})

	// After timing out, timer should be reset to initialTimeout, rather than
	// activity timeout, so prompt should expire after initialTimeout (since we
	// already elapsed initialTimeout-1ns, just wait 1ns more).
	timer.Elapse(time.Nanosecond)
	checkCurrentNotices(c, noticeChan, prompt.ID, map[string]string{"resolved": "expired"})
	allowedPerms = waitForReply(c, replyChan4)
	c.Assert(allowedPerms, DeepEquals, []string{"read"})
	c.Assert(timer.FireCount(), Equals, 3)
	expectedMap = map[string]requestprompts.RequestMapEntry{}
	s.checkWrittenRequestMap(c, expectedMap)
}

func (s *requestpromptsSuite) TestPromptExpirationRace(c *C) {
	callbackSignaller := make(chan bool, 0)
	var timer *testtime.TestTimer
	restore := requestprompts.MockTimeAfterFunc(func(d time.Duration, f func()) timeutil.Timer {
		if timer != nil {
			c.Fatalf("created more than one timer")
		}
		callback := func() {
			// Wait for a signal over startCallback
			<-callbackSignaller
			f()
			callbackSignaller <- true
		}
		timer = testtime.AfterFunc(d, callback)
		return timer
	})
	defer restore()

	metadata := &prompting.Metadata{
		User:      s.defaultUser,
		Snap:      "firefox",
		PID:       123,
		Cgroup:    "0::/user.slice/user-1000.slice/user@1000.service/app.slice/some-cgroup.scope",
		Interface: "home",
	}
	path := "/home/test/foo"
	requestedPermissions := []string{"read", "write", "execute"}
	outstandingPermissions := []string{"write", "execute"}

	noticeChan := make(chan noticeInfo, 1)
	pdb, err := requestprompts.New(func(userID uint32, promptID prompting.IDType, data map[string]string) error {
		c.Assert(userID, Equals, s.defaultUser)
		noticeChan <- noticeInfo{
			promptID: promptID,
			data:     data,
		}
		return nil
	})
	c.Assert(err, IsNil)
	defer pdb.Close()

	// Add prompt
	req, replyChan := newRequestWithReplyChan("fake:1")
	prompt, merged, err := pdb.AddOrMerge(metadata, path, requestedPermissions, outstandingPermissions, req)
	c.Assert(err, IsNil)
	c.Assert(merged, Equals, false)
	checkCurrentNotices(c, noticeChan, prompt.ID, nil)

	// Check that prompt has not immediately expired
	c.Assert(timer.FireCount(), Equals, 0)

	// Cause prompt to timeout, but the callback will wait for a signal, so we
	// can reset it, simulating activity occurring just as the timer fires.
	timer.Elapse(requestprompts.InitialTimeout)

	// Check that the timer fired
	c.Assert(timer.FireCount(), Equals, 1)

	// Reset timer to half of initial timeout, as if activity occurred, but it's
	// easier to check that the timeout was correctly reset to activityTimeout
	// if the preemptive reset was not also to activityTimeout.
	//
	// In the real world, what would have happened is that activity occurred
	// just as the timer fired, thus resetting the timer to activityTimeout
	// just before the timeout callback sets it to initialTimeout, and we want
	// to ensure that the callback correctly detects that the activity had
	// occurred (by the timer being active again) and overrides its own just-set
	// initialTimeout by resetting the timer back to activityTimeout.
	timer.Reset(requestprompts.InitialTimeout / 2)

	// Start the actual callback
	callbackSignaller <- true
	// Wait for the callback to complete
	<-callbackSignaller

	// Check that prompt has not expired
	clientActivity := false
	retrieved, err := pdb.PromptWithID(s.defaultUser, prompt.ID, clientActivity)
	c.Assert(err, IsNil)
	c.Assert(retrieved, Equals, prompt)
	c.Assert(timer.FireCount(), Equals, 1)

	// Check that the callback correctly identified that the timer had been
	// reset prior to the callback doing so, and thus re-reset the timeout to
	// activityTimeout instead of leaving it reset to initialTimeout.
	// First, check that the prompt doesn't expire after the preemptively reset
	// timeout (but before initialTimeout).
	timer.Elapse(requestprompts.InitialTimeout - requestprompts.InitialTimeout/4)
	c.Assert(timer.FireCount(), Equals, 1)
	// Next, check that the prompt doesn't expire after the full initialTimeout
	// (but before activityTimeout).
	timer.Elapse(requestprompts.InitialTimeout / 4)
	c.Assert(timer.FireCount(), Equals, 1)

	// Check that the prompt does expire after the total activityTimeout
	// following a race while the timeout was firing
	timer.Elapse(requestprompts.ActivityTimeout - requestprompts.InitialTimeout)
	c.Assert(timer.FireCount(), Equals, 2)

	// Allow the callback to run
	callbackSignaller <- true
	// Wait for it to finish
	<-callbackSignaller

	checkCurrentNotices(c, noticeChan, prompt.ID, map[string]string{"resolved": "expired"})
	allowedPerms := waitForReply(c, replyChan)
	c.Check(allowedPerms, DeepEquals, []string{"read"})

	_, err = pdb.PromptWithID(s.defaultUser, prompt.ID, clientActivity)
	c.Assert(err, Equals, prompting_errors.ErrPromptNotFound)
}

func checkCurrentNotices(c *C, noticeChan chan noticeInfo, expectedID prompting.IDType, expectedData map[string]string) {
	select {
	case info := <-noticeChan:
		c.Assert(info.promptID, Equals, expectedID)
		c.Assert(info.data, DeepEquals, expectedData)
	case <-time.NewTimer(10 * time.Second).C:
		c.Fatal("no notices")
	}
}

func checkCurrentNoticesMultiple(c *C, noticeChan chan noticeInfo, expectedIDs []prompting.IDType, expectedData map[string]string) {
	expected := make(map[prompting.IDType]int)
	for _, id := range expectedIDs {
		expected[id] += 1
	}
	seen := make(map[prompting.IDType]int)
	for range expectedIDs {
		select {
		case info := <-noticeChan:
			seen[info.promptID] += 1
			c.Assert(info.data, DeepEquals, expectedData)
		case <-time.NewTimer(10 * time.Second).C:
			c.Fatal("no notices")
		}
	}
	c.Assert(seen, DeepEquals, expected)
}

func waitForReply(c *C, replyChan <-chan []string) []string {
	select {
	case allowedPermissions := <-replyChan:
		return allowedPermissions
	case <-time.NewTimer(10 * time.Second).C:
		c.Fatalf("timed out waiting for reply")
	}
	// cannot reach here
	return nil
}

func waitForReplies(c *C, replyChan1, replyChan2 <-chan []string) ([]string, []string) {
	var res1 []string
	var res2 []string
	select {
	case allowedPerms := <-replyChan1:
		res1 = allowedPerms
	case allowedPerms := <-replyChan2:
		res2 = allowedPerms
	case <-time.NewTimer(10 * time.Second).C:
		c.Fatalf("timed out waiting for reply")
	}
	select {
	case allowedPerms := <-replyChan1:
		res1 = allowedPerms
	case allowedPerms := <-replyChan2:
		res2 = allowedPerms
	case <-time.NewTimer(10 * time.Second).C:
		c.Fatalf("timed out waiting for reply")
	}
	return res1, res2
}
