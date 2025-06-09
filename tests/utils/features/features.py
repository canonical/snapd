
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
    done = "Done"
    undone = "Undone"
    error = "Error"

class Task(TypedDict):
    id: str
    kind: str
    snap_types: list[str]
    last_status: str


class Change(TypedDict):
    kind: str
    snap_types: list[str]


class Ensure(TypedDict):
    manager: str
    function: str


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


class CmdLogLine:
    cmd = 'cmd'


class EndpointLogLine:
    method = 'method'
    path = 'path'
    action = 'action'


class InterfaceLogLine:
    interface = 'interface'
    slot = 'slot'
    plug = 'plug'


class EnsureLogLine:
    manager = 'manager'
    func = 'func'


class TaskLogLine:
    task_name = 'task-name'
    id = 'id'
    status = 'status'


class ChangeLogLine:
    kind = 'kind'
    id = 'id'
