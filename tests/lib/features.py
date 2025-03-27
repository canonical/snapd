
'''
Dictionaries to specify structure of feature logs
'''

from enum import Enum
from typing import TypedDict


class Cmd(TypedDict):
    cmd: str


class Endpoint(TypedDict):
    method: str
    path: str
    action: str


class Interface(TypedDict):
    name: str
    plug_snap_type: str
    slot_snap_type: str


class Status(str, Enum):
    done = "done"
    undone = "undone"
    error = "error"


class Task(TypedDict):
    kind: str
    snap_type: str
    last_status: Status


class Change(TypedDict):
    kind: str
    snap_type: str


class Ensure(TypedDict):
    functions: list[str]


class EnvVariables(TypedDict):
    name: str
    value: str

class TaskFeatures(TypedDict):
    suite: str
    task_name: str
    variant: str
    success: bool
    cmds: list[Cmd]
    endpoints: list[Endpoint]
    interfaces: list[Interface]
    tasks: list[Task]
    changes: list[Change]
    ensures: list[Ensure]


class SystemFeatures(TypedDict):
    schema_version: str
    system: str
    scenarios: list[str]
    env_variables: list[EnvVariables]
    tests: list[TaskFeatures]
