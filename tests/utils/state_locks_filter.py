#!/usr/bin/env python3

import argparse
import re
import sys


class LockOpTrace:
    """
    LockOpTrace: Represents a Lock Operation trace in the log file.
    It holds the lines for the trace and allows matching against a part of the trace.
    """

    def __init__(self, lines: list[str]):
        self.lines = lines
        self.hash = hash(str(self))

    def get_trace_lines(self) -> list[str]:
        return self.lines

    def match(self, part: str) -> bool:
        for line in self.lines:
            if part in line:
                return True

        return False

    def __str__(self) -> str:
        return "".join(self.lines)

    def __hash__(self):
        return self.hash

    def __eq__(self, other) -> bool:
        if not isinstance(other, LockOpTrace):
            # don't attempt to compare against unrelated types
            return NotImplemented

        return hash(str(self)) == hash(str(other))



class LockOp:
    """
    LockOp: Represents a lock operation in the log file, including its header and times (held and wait times).
    It is tied to the LockOpTrace class.
    """

    def __init__(self, lines: list[str]):
        self.trace = LockOpTrace(lines[1:])
        self.header = lines[0]
        self.held_time = 0
        self.wait_time = 0
        self._calc_held_ms(self.header)
        self._calc_wait_ms(self.header)

    def _calc_held_ms(self, line: str) -> int:
        match = re.search(".*held: (.+?) ms.*", line)
        if match:
            self.held_time = int(match.group(1))

    def _calc_wait_ms(self, line: str) -> int:
        match = re.search(".*wait (.+?) ms.*", line)
        if match:
            self.wait_time = int(match.group(1))

    def get_held_time(self) -> int:
        return self.held_time

    def get_wait_time(self) -> int:
        return self.wait_time

    def get_trace(self) -> LockOpTrace:
        return self.trace


class LocksGroup:
    """
    LocksGroup: Represents a group of locks, which could correspond to a test case or a project setup.
    It holds a header (name) and a list of Lock objects.
    """

    LOCK_PREFIX = "### "

    locks: list[LockOp]

    def __init__(self, lines: list[str]):
        self.lines = lines
        self.header = self.lines[0]
        self.locks = []

        self._read()

    def _read(self):
        current_line = 1
        while True:
            if current_line >= len(self.lines):
                return

            lock_lines = self._current_lock(current_line)
            if len(lock_lines) == 0:
                raise RuntimeError("Error parsing lock")

            self.locks.append(LockOp(lock_lines))
            current_line = current_line + len(lock_lines)

    def __str__(self) -> str:
        return "".join(self.lines)

    # This function works to detect the current test but also for the current project
    def _current_lock(self, start_line: int) -> list[str]:
        next_match = get_next_match(self.lines, start_line, self.LOCK_PREFIX)
        if next_match == -1:
            return self.lines[start_line:len(self.lines)]
        return self.lines[start_line:next_match]

    def get_name(self) -> str:
        return self.header

    def get_locks(self) -> list[LockOp]:
        return self.locks

    def get_traces(self) -> list[LockOpTrace]:
        traces = []
        for lock in self.locks:
            traces.append(lock.get_trace())
        return traces

    def get_lock_held_time(self, trace: LockOpTrace) -> int:
        for lock in self.locks:
            if lock.get_trace() == trace:
                return lock.get_held_time()

        return 0

    def get_lock_wait_time(self, trace: LockOpTrace) -> int:
        for lock in self.locks:
            if lock.get_trace() == trace:
                return lock.get_wait_time()

        return 0


class GroupTimes:
    """
    GroupTimes: A tuple-like class that associates the group name with the held and wait times for each lock trace.
    """

    def __init__(self, group_name: str, held_time: int, wait_time: int):
        self.group_name = group_name
        self.held_time = held_time
        self.wait_time = wait_time

    def get_group_name(self) -> str:
        return self.group_name

    def get_held_time(self) -> int:
        return self.held_time

    def get_wait_time(self) -> int:
        return self.wait_time


class LockTraceManager:
    """
    LockTraceManager: Handles filtering and managing the lock traces and associated group times.
    It provides methods to filter the traces by time and to print the results in a sorted manner.
    """

    def __init__(self, traces: dict[LockOpTrace, list[GroupTimes]]):
        self.traces = traces

    # Filter the times for each trace
    def filter(self, held_time: int, wait_time: int):
        filtered_traces = dict[LockOpTrace, list[GroupTimes]]()
        for trace, times in self.traces.items():
            filtered_times = [
                time
                for time in self.traces.get(trace)
                if time.get_held_time() >= held_time
                and time.get_wait_time() >= wait_time
            ]
            if len(filtered_times) > 0:
                filtered_traces[trace] = filtered_times

        self.traces = filtered_traces

    # Keep the traces that match with the params
    def match(self, match_names: list[str]):
        filtered_traces = dict[LockOpTrace, list[GroupTimes]]()
        for trace, times in self.traces.items():
            for match_name in match_names:
                if trace.match(match_name):
                    filtered_traces[trace] = times

        self.traces = filtered_traces

    # print the traces with their times for each test
    def print(self, sort_held_time, sort_wait_time):
        if sort_held_time:
            for trace, times in self.traces.items():
                self.traces[trace] = sorted(
                    times, key=lambda x: x.get_held_time(), reverse=True
                )
        if sort_wait_time:
            for trace, times in self.traces.items():
                self.traces[trace] = sorted(
                    times, key=lambda x: x.get_held_time(), reverse=True
                )

        for trace, times in self.traces.items():
            print("-" * 20 + "TRACE" + "-" * 20)
            print("")
            print(trace)
            print("")
            for time in times:
                print(
                    "{} held: {} ms, wait: {} ms".format(
                        time.get_group_name(),
                        time.get_held_time(),
                        time.get_wait_time(),
                    )
                )
            print("")


class LocksFileReader:
    """
    LocksFileReader: Reads the lock file and parses the different test cases and groups.
    It extracts the relevant trace data and stores it in LocksGroup instances.
    It can also return a dictionary of traces and associated group times.
    """

    PROJECT_PREFIX = "###START: SNAPD PROJECT"
    TEST_PREFIX = "###START:"

    lines: list[str]
    groups: list[LocksGroup]

    def __init__(self, locks_file: argparse.FileType):
        self.lines = []
        self.groups = []

        self._read(locks_file)

    def _read(self, locks_file: argparse.FileType):
        self.lines =  locks_file.readlines()

        current_line = 0
        if not self._is_project(self.lines[current_line]):
            print("First time expected to be the project start.")
            sys.exit(1)

        # Read the tests
        while True:
            if current_line >= len(self.lines):
                return

            group_lines = self._current_group(current_line)
            if len(group_lines) == 0:
                raise RuntimeError("Error parsing test.")

            self.groups.append(LocksGroup(group_lines))
            current_line = current_line + len(group_lines)

    # Indicates if the line is the project declaration
    def _is_project(self, line: str) -> bool:
        return line.startswith(self.PROJECT_PREFIX)

    # This function works to detect the current test but also for the current project
    def _current_group(self, start_line: int) -> list[str]:
        next_match = get_next_match(self.lines, start_line, self.TEST_PREFIX)
        if next_match == -1:
            return self.lines[start_line:len(self.lines)]
        return self.lines[start_line:next_match]

    # Retrieve the test lines
    def get_test(self, test: str) -> list[str]:
        for group in self.groups:
            if test in group.get_name():
                return str(group)

        return ""

    # Retrieve the times for each trace in the file
    # For each trace, there is a list with the times for each test where the trace appears
    def get_traces_times(self) -> dict[LockOpTrace, list[GroupTimes]]:
        traces = dict[LockOpTrace, list[GroupTimes]]()
        for group in self.groups:
            group_traces = group.get_traces()
            for trace in group_traces:
                group_time = GroupTimes(
                    group.get_name(),
                    group.get_lock_held_time(trace),
                    group.get_lock_wait_time(trace),
                )
                if not trace in traces.keys():
                    traces[trace] = list[GroupTimes]()
                traces.get(trace).append(group_time)

        return traces


"""
Generic function used to iterate a list of strings until it is found an
element which starts with the prefix passed as parameter. The function
returns the index for the first match, and -1 in case there isn't any.
"""
def get_next_match(lines: list[str], start: int, prefix: str) -> int:
    for current_line in range(start+1, len(lines)):
        if lines[current_line].startswith(prefix):
            return current_line
    return -1


"""
A locks file must:
- begin with "###START: SNAPD PROJECT"
- contain at least one group, indicated by the key string "###START"

It parses the lock information by:
- grouping its contents based on consecutive lines that begin with "###START"
- creating subgroups for each group by searching for the key string "###" and creating a subgroup for each instance found of consecutive lines between instances of that key string

One may filter data based on:
- matching string found in a sequence of sub-group lock information. If a match is found, then only sub-groups that contain that match will be shown
- only showing held or wait times above a certain threshold
- a singular test
"""
def _make_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="state-locks-filter", 
                                     description="Reads a locks file and extracts lock information.")
    parser.add_argument(
        "-f", "--locks-file", type=argparse.FileType("r"), required=True, help="Locks file"
    )
    parser.add_argument(
        "--test",
        default="",
        help="Show results for this test, when test is selected no other filters are applied.",
    )
    parser.add_argument(
        "--match",
        action="append",
        default=[],
        help="Show traces that match this specific word",
    )
    parser.add_argument(
        "--held-time", type=int, default=0, help="Include locks with longer held time"
    )
    parser.add_argument(
        "--wait-time", type=int, default=0, help="Include locks with longer wait time"
    )
    parser.add_argument(
        "--sort-held-time",
        action="store_true",
        help="Sort higher times first by held time",
    )
    parser.add_argument(
        "--sort-wait-time",
        action="store_true",
        help="Sort higher times first by wait time",
    )

    return parser


if __name__ == "__main__":
    parser = _make_parser()
    args = parser.parse_args()

    if args.sort_held_time and args.sort_wait_time:
        print("state-lock-filter: define just 1 sorting (by held or wait times)")
        sys.exit(1)

    locks_reader = LocksFileReader(args.locks_file)
    if args.test:
        print(locks_reader.get_test(args.test))
        sys.exit()

    trace_manager = LockTraceManager(locks_reader.get_traces_times())

    # Then keep traces with matches
    if args.match:
        trace_manager.match(args.match)

    # First filter by time
    if args.held_time > 0 or args.wait_time > 0:
        trace_manager.filter(args.held_time, args.wait_time)

    # And finally print the sorted results (if required)
    trace_manager.print(args.sort_held_time, args.sort_wait_time)
