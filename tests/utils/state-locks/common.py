#!/usr/bin/env python3

from __future__ import annotations


class LockOpTrace:
    """
    LockOpTrace: Represents a Lock Operation trace in the log file.
    It holds the lines for the trace and allows matching against a part of
    the trace.
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

    def print(self):
        print("-" * 20 + "TRACE" + "-" * 20)
        print("")
        print(self)
        print("")

    def __str__(self) -> str:
        return "".join(self.lines).rstrip()

    def __hash__(self) -> int:
        return self.hash

    def __eq__(self, other: object) -> bool:
        if not isinstance(other, LockOpTrace):
            # don't attempt to compare against unrelated types
            return NotImplemented

        return self.hash == other.hash


"""
Generic function used to iterate a list of strings until it is found an
element which starts with the prefix passed as parameter. The function
returns the index for the first match, and -1 in case there isn't any.
"""


def get_next_match(lines: list[str], start: int, prefix: str) -> int:
    for current_line in range(start + 1, len(lines)):
        if lines[current_line].startswith(prefix):
            return current_line
    return -1
