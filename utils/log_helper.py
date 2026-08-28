"""
This library provides a set of functions that can be used to
process the spread output.
"""

import re

from enum import Enum


class ListedEnum(Enum):
    @classmethod
    def list(cls) -> list[str]:
        return list(map(lambda c: c.value, cls))

class ExecutionPhase(ListedEnum):
    PREPARING = "Preparing"
    EXECUTING = "Executing"
    RESTORING = "Restoring"


class ExecutionInfo(ListedEnum):
    DEBUG = "Debug"
    ERROR = "Error"
    WARNING = "WARNING:"


class ExecutionLevel(ListedEnum):
    TASK = "task"
    SUITE = "suite"
    PROJECT = "project"


class GeneralAction(ListedEnum):
    REBOOTING = "Rebooting"
    DISCARDING = "Discarding"
    ALLOCATING = "Allocating"
    WAITING = "Waiting"
    CONNECTING = "Connecting"
    SENDING = "Sending"


class GeneralActionStatus(ListedEnum):
    ALLOCATED = "Allocated"
    CONNECTED = "Connected"


class Result(ListedEnum):
    FAILED = "Failed"
    SUCCESSFUL = "Successful"
    ABORTED = "Aborted"


OPERATIONS = (
    ExecutionPhase.list()
    + GeneralAction.list()
    + GeneralActionStatus.list()
    + ExecutionInfo.list()
    + Result.list()
)


# Start line
START_LINE = ".*Project content is packed for delivery.*"


def _match_date(date: str) -> bool:
    return re.match(r"\d{4}-\d{2}-\d{2}", date) is not None


def _match_time(time: str) -> bool:
    return re.match(r"\d{2}:\d{2}:\d{2}", time) is not None


def is_initial_line(line: str) -> bool:
    if not line:
        return False

    pattern = re.compile(START_LINE)
    parts = line.strip().split(" ")
    return (
        len(parts) > 2
        and _match_date(parts[0])
        and _match_time(parts[1])
        and re.match(pattern, line) is not None
    )


# Debug line starts with the operation
def is_operation(line: str, operation: Enum) -> bool:
    if not line:
        return False

    parts = line.strip().split(" ")
    return (
        len(parts) > 2
        and _match_date(parts[0])
        and _match_time(parts[1])
        and parts[2] == operation.value
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
        is_operation(line, ExecutionInfo.DEBUG)
        or is_operation(line, ExecutionInfo.ERROR)
        or is_operation(line, ExecutionInfo.WARNING)
        or is_operation(line, Result.FAILED)
    )


# Error/Debug/Failed output sometimes finish with either EOF error or a log file
# and no detail displayed
def is_detail(line: str) -> bool:
    return (
        is_operation(line, ExecutionInfo.DEBUG)
        or is_operation(line, ExecutionInfo.ERROR)
        or is_operation(line, ExecutionInfo.WARNING)
    ) and line.strip()[-1:] == ":"


# Error/Debug/Failed output finishes when a new other line starts
def is_detail_finished(line: str) -> bool:
    parts = line.strip().split(" ")
    return (
        len(parts) > 3
        and _match_date(parts[0])
        and _match_time(parts[1])
        and parts[2] in OPERATIONS
    )
