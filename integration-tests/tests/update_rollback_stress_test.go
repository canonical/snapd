// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration,!excludereboots,rollbackstress

/*
 * Copyright (C) 2016 Canonical Ltd
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

package tests

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"

	"gopkg.in/check.v1"
)

const (
	totalCycles     = 3
	updateMarker    = "update"
	rollbackMarker  = "rollback"
	artifactsDir    = "update-rollback-stress"
	rollbackVerFile = "rollbackedVersion"
	updateVerFile   = "updatedVersion"
)

var _ = check.Suite(&updateRollbackSuite{})

var basePath = filepath.Join(os.Getenv("ADT_ARTIFACTS"), artifactsDir)

type updateRollbackSuite struct {
	common.SnappySuite
	cm *cycleManager
}

func (s *updateRollbackSuite) SetUpTest(c *check.C) {
	s.SnappySuite.SetUpTest(c)

	err := os.MkdirAll(basePath, 0777)
	c.Assert(err, check.IsNil)

	currentVersion := common.GetCurrentUbuntuCoreVersion(c)
	s.cm, err = newCycleManager(currentVersion)
	c.Assert(err, check.IsNil, check.Commentf("Error creating manager: %v", err))
}

type fileManager struct {
	basePath string
}

func (fm *fileManager) get(subpath string) (string, error) {
	dat, err := ioutil.ReadFile(fm.fullPath(subpath))
	if os.IsNotExist(err) {
		return "", nil
	}
	return string(dat), err
}

func (fm *fileManager) save(subpath, value string) error {
	dat := []byte(value)
	return ioutil.WriteFile(fm.fullPath(subpath), dat, 0644)
}

func (fm *fileManager) fullPath(subpath string) string {
	return filepath.Join(fm.basePath, subpath)
}

type cycleManager struct {
	cycle                                             int
	status                                            string
	currentVersion, rollbackedVersion, updatedVersion string
	fm                                                *fileManager
}

func newCycleManager(version string) (*cycleManager, error) {
	fm := &fileManager{basePath: basePath}

	status, err := fm.get("status")
	if err != nil {
		return nil, err
	}
	cycleStr, err := fm.get("cycle")
	if err != nil {
		return nil, err
	}
	var cycle int
	if cycleStr != "" {
		cycle, err = strconv.Atoi(cycleStr)
		if err != nil {
			return nil, err
		}
	}
	return &cycleManager{
		status:         status,
		cycle:          cycle,
		currentVersion: version,
		fm:             fm,
	}, nil
}

func (cm *cycleManager) isUpdateStatus() bool {
	return cm.status == updateMarker
}

func (cm *cycleManager) isRollbackStatus() bool {
	// we assume that the image has been created in an updatable status (we deal with first
	// time and rollback similarly)
	return cm.status == "" || cm.status == rollbackMarker
}

func (cm *cycleManager) isDone() bool {
	return cm.cycle >= totalCycles
}

func (cm *cycleManager) checkUpdatedVersion() (bool, error) {
	var err error
	cm.updatedVersion, err = cm.getSavedUpdatedVersion()
	return cm.updatedVersion == "" || cm.currentVersion != cm.updatedVersion, err
}

func (cm *cycleManager) checkRollbackedVersion() (bool, error) {
	var err error
	cm.rollbackedVersion, err = cm.getSavedRollbackedVersion()
	return cm.rollbackedVersion == "" || cm.currentVersion != cm.rollbackedVersion, err
}

func (cm *cycleManager) getSavedStatus() (string, error) {
	return cm.fm.get("status")
}

func (cm *cycleManager) getSavedCycle() (int, error) {
	cycle, err := cm.fm.get("cycle")
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(cycle)
}

func (cm *cycleManager) saveNextCycle() error {
	cm.cycle++
	return cm.fm.save("cycle", strconv.Itoa(cm.cycle))
}

func (cm *cycleManager) saveNextStatus() error {
	var status string
	if cm.status == updateMarker {
		status = rollbackMarker
	} else {
		status = updateMarker
	}
	return cm.fm.save("status", status)
}

func (cm *cycleManager) getSavedRollbackedVersion() (string, error) {
	return cm.fm.get(rollbackVerFile)
}

func (cm *cycleManager) getSavedUpdatedVersion() (string, error) {
	return cm.fm.get(updateVerFile)
}

func (cm *cycleManager) saveRollbackedVersion() error {
	return cm.fm.save(rollbackVerFile, cm.rollbackedVersion)
}

func (cm *cycleManager) saveUpdatedVersion() error {
	return cm.fm.save(updateVerFile, cm.updatedVersion)
}

func (s *updateRollbackSuite) TestUpdateRollbackStress(c *check.C) {
	if s.cm.isRollbackStatus() {
		// first time or after rollback
		doAfterRollbackActions(c, s.cm)

		cli.ExecCommand(c, "sudo", "snappy", "update")

	} else if s.cm.isUpdateStatus() {
		// after update
		doAfterUpdateActions(c, s.cm)

		cli.ExecCommand(c, "sudo", "snappy", "rollback", "ubuntu-core")

	} else {
		c.Log("Unknown update-rollback status:", s.cm.status)
		c.FailNow()
	}
	common.Reboot(c)
}

func doAfterRollbackActions(c *check.C, cm *cycleManager) {
	ch, err := cm.checkRollbackedVersion()
	c.Assert(ch, check.Equals, true,
		check.Commentf("Error checking version, current version %s should be equal to rollback version %s",
			cm.currentVersion, cm.rollbackedVersion))
	c.Assert(err, check.IsNil, check.Commentf("Error checking version %v", err))

	err = cm.saveNextStatus()
	c.Assert(err, check.IsNil)
	err = cm.saveRollbackedVersion()
	c.Assert(err, check.IsNil)
}

func doAfterUpdateActions(c *check.C, cm *cycleManager) {
	if cm.isDone() {
		c.SucceedNow()
	}
	ch, err := cm.checkUpdatedVersion()
	c.Assert(ch, check.Equals, true,
		check.Commentf("Error checking version, current version %s should be equal to updated version %s",
			cm.currentVersion, cm.updatedVersion))
	c.Assert(err, check.IsNil, check.Commentf("Error checking version %v", err))

	err = cm.saveNextStatus()
	c.Assert(err, check.IsNil)
	err = cm.saveNextCycle()
	c.Assert(err, check.IsNil)
	err = cm.saveUpdatedVersion()
	c.Assert(err, check.IsNil)
}
