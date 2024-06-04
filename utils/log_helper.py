"""
This library provides a set of functions that can be used to
process the spread output.
"""

import re

# Tasks status
PREPARING = "Preparing"
EXECUTING = "Executing"
RESTORING = "Restoring"

# Tasks info
DEBUG = "Debug"
ERROR = "Error"
WARNING = "WARNING:"

# General actions
REBOOTING = "Rebooting"
DISCARDING = "Discarding"
ALLOCATING = "Allocating"
WAITING = "Waiting"
CONNECTING = "Connecting"
SENDING = "Sending"

# Actions status
ALLOCATED = "Allocated"
CONNECTED = "Connected"

# Results
FAILED = "Failed"
SUCCESSFUL = "Successful"
ABORTED = "Aborted"

# Tasks levels
TASK = "task"
SUITE = "suite"
PROJECT = "project"

# Start line
START_LINE = ".*Project content is packed for delivery.*"

OPERATIONS = [
    PREPARING,
    EXECUTING,
    RESTORING,
    REBOOTING,
    DISCARDING,
    ALLOCATING,
    WAITING,
    ALLOCATED,
    CONNECTING,
    CONNECTED,
    SENDING,
    ERROR,
    DEBUG,
    WARNING,
    FAILED,
    SUCCESSFUL,
    ABORTED,
]


def _match_date(date):
    return re.match(r"\d{4}-\d{2}-\d{2}", date)


def _match_time(time):
    return re.match(r"\d{2}:\d{2}:\d{2}", time)


def is_initial_line(line: str) -> bool:
    if not line:
        return False

    pattern = re.compile(START_LINE)
    parts = line.strip().split(" ")
    return (
        len(parts) > 2
        and _match_date(parts[0])
        and _match_time(parts[1])
        and re.match(pattern, line)
    )


# Debug line starts with the operation
def is_operation(line: str, operation: str) -> bool:
    if not line:
        return False

    parts = line.strip().split(" ")
    return (
        len(parts) > 2
        and _match_date(parts[0])
        and _match_time(parts[1])
        and parts[2] == operation
    )


# Check if the line contains any operation
def is_any_operation(line: str) -> bool:
    if not line:
        return False

    parts = line.strip().split(" ")
    return (
        len(parts) > 2
        and _match_date(parts[0])
        and _match_time(parts[1])
        and parts[2] in OPERATIONS
    )


# Return the date in the line
def get_date(line: str) -> str:
    if not line:
        raise RuntimeError("Empty line provided")

    parts = line.strip().split(" ")
    if len(parts) <= 2 or not _match_date(parts[0]):
        raise ValueError("Incorrect format for line provided: {}".format(line))

    return parts[0]


# Return the time in the line
def get_time(line: str) -> str:
    if not line:
        raise RuntimeError("Empty line provided")

    parts = line.strip().split(" ")
    if len(parts) <= 2 or not _match_date(parts[0]) or not _match_time(parts[1]):
        raise ValueError("Incorrect format for line provided: {}".format(line))

    return parts[1]


# Return the operation in the line
def get_operation(line: str) -> str:
    if not line:
        raise RuntimeError("Empty line provided")

    parts = line.strip().split(" ")
    if len(parts) <= 2 or not _match_date(parts[0]) or not _match_time(parts[1]):
        raise ValueError("Incorrect format for line provided: {}".format(line))

    return parts[2]


# Return the information that comes after the operation
def get_operation_info(line: str) -> str:
    if not line:
        raise RuntimeError("Empty line provided")

    parts = line.strip().split(" ")
    if len(parts) <= 3 or not _match_date(parts[0]) or not _match_time(parts[1]):
        raise ValueError("Incorrect format for line provided: {}".format(line))

    return " ".join(parts[3:])


# Details are displayed after Error/Debug/Failed/Warning operations
def is_detail_start(line: str) -> bool:
    return (
        is_operation(line, DEBUG)
        or is_operation(line, ERROR)
        or is_operation(line, WARNING)
        or is_operation(line, FAILED)
    )


# Error/Debug/Failed output sometimes finish witr EOF error
def is_detail_eof(line: str) -> bool:
    parts = line.strip().split(" ")
    return (
        is_operation(line, DEBUG)
        or is_operation(line, ERROR)
        or is_operation(line, WARNING)
    ) and parts[-1].strip() == "EOF"


# Error/Debug/Failed output finishes when a new other line starts
def is_detail_finished(line: str) -> bool:
    parts = line.strip().split(" ")
    return (
        len(parts) > 3
        and _match_date(parts[0])
        and _match_time(parts[1])
        and parts[2] in OPERATIONS
    )
