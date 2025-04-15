#!/usr/bin/env python3

from __future__ import annotations

import argparse
from typing import IO

from common import LockOpTrace, get_next_match


class LockOpTraceFileReader:
    """
    LocksFileReader: Reads the traces file and retrieves differences
    with a second traces file
    """

    lines: dict[LockOpTrace, LockOpTrace]

    def __init__(self, traces_file: IO[str]):
        self.traces = {}
        self.lines = []

        self._read(traces_file)

    def _read(self, traces_file: IO[str]) -> None:
        self.lines = traces_file.readlines()

        current_line = 0
        # Read the tests
        while current_line < len(self.lines):
            trace_lines = self._current_trace(current_line)
            if len(trace_lines) == 0:
                raise RuntimeError("Error parsing traces file")

            # Remove empty lines and title
            cleaned_lines = [
                line for line in trace_lines if line.strip() and
                "TRACE" not in line
            ]

            # Remove the file and line numbers
            # Line numbers could easily change over time
            # File paths could include snap versions which used to change
            function_lines = [line.split(" ", 1)[1] for line in cleaned_lines]

            # Save the traces in a dict to be able to print the version of the trace
            # with the file and line number which is not used to compare
            self.traces[LockOpTrace(function_lines)] = LockOpTrace(cleaned_lines)
            current_line = current_line + len(trace_lines)

    # This function works to detect the current trace
    def _current_trace(self, start_line: int) -> list[str]:
        next_match = get_next_match(self.lines, start_line, "---")
        if next_match == -1:
            return self.lines[start_line:len(self.lines)]
        return self.lines[start_line:next_match]

    # Retieve the traces which are in other and are not in self
    def get_diff(self, other: LockOpTraceFileReader) -> list[LockOpTrace]:
        diff_traces = []
        my_traces = self.traces.keys()
        for key_trace, val_trace in other.traces.items():
            if key_trace not in my_traces:
                diff_traces.append(val_trace)
        return diff_traces

    # Print the traces which are in other and are not in self
    def print_diff(self, other: LockOpTraceFileReader):
        for trace in self.get_diff(other):
            trace.print()


"""
A traces file is a sequence of traces separated by an empty line

It parses the traces file and will print the traces included in the
locks-file which are not present in the baseline.
"""


def _make_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="state-locks-filter",
        description="Reads a locks file and extracts lock information.",
    )
    parser.add_argument(
        "-b",
        "--baseline-file",
        type=argparse.FileType("r"),
        required=True,
        help="Baseline traces file",
    )
    parser.add_argument(
        "-f",
        "--locks-file",
        type=argparse.FileType("r"),
        required=True,
        help="Test traces file",
    )

    return parser


if __name__ == "__main__":
    parser = _make_parser()
    args = parser.parse_args()

    locks_reader_baseline = LockOpTraceFileReader(args.baseline_file)
    locks_reader_locks = LockOpTraceFileReader(args.locks_file)
    locks_reader_baseline.print_diff(locks_reader_locks)
