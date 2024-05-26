// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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

package cgroup

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/ddkwork/golibrary/mylog"
)

// pidsInFile returns the list of process IDs in a given file.
func pidsInFile(fname string) ([]int, error) {
	file := mylog.Check2(os.Open(fname))
	if os.IsNotExist(err) {
		return nil, nil
	}

	defer file.Close()
	return parsePids(bufio.NewReader(file))
}

// parsePids parses a list of pids, one per line, from a reader.
func parsePids(reader io.Reader) ([]int, error) {
	scanner := bufio.NewScanner(reader)
	var pids []int
	for scanner.Scan() {
		s := scanner.Text()
		pid := mylog.Check2(parsePid(s))

		pids = append(pids, pid)
	}
	mylog.Check(scanner.Err())

	return pids, nil
}

// parsePid parses a string as a process identifier.
func parsePid(text string) (int, error) {
	pid := mylog.Check2(strconv.Atoi(text))
	if err != nil || (err == nil && pid <= 0) {
		return 0, fmt.Errorf("cannot parse pid %q", text)
	}
	return pid, err
}
