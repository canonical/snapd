
from typing import Any

CHANGES = 'changes'
TASKS = 'tasks'
SNAP = 'snap'
SNAPS = 'snaps'
TYPE = 'type'
NAME = 'name'
PLUG = 'plug'
SLOT = 'slot'
SNAP_NAME = 'snap-name'
SNAP_TYPE = 'snap-type'
SNAP_SETUP = 'snap-setup'
SNAP_SETUP_TASK = 'snap-setup-task'
SNAP_SETUP_TASKS = 'snap-setup-tasks'
HOOK_SETUP = 'hook-setup'
SIDE_INFO = 'side-info'
DATA = 'data'
SERVICE_ACTION = 'service-action'
QUOTA_CONTROL_ACTIONS = 'quota-control-actions'
RECOVERY_SYSTEM_SETUP_TASK = 'recovery-system-setup-task'
RECOVERY_SYSTEM_SETUP = 'recovery-system-setup'
SNAPSHOT_SETUP = 'snapshot-setup'


TASKS_IGNORE_SNAP_TYPE = {
    # These tasks are not associated with a particular snap
    'clear-confdb-tx-on-error',
    'commit-confdb-tx',
    'clear-confdb-tx',
    'load-confdb-change',
    'update-gadget-cmdline',
    'create-recovery-system',
    'finalize-recovery-system',
    'remove-recovery-system',
    'check-rerefresh',
    'exec-command',
    'request-serial',
    'enforce-validation-sets',
    'mark-seeded',
    'hotplug-add-slot',
    'hotplug-connect',
    'hotplug-disconnect',
    'hotplug-remove-slot',

    # These two are only associated with app/gadget types
    # of snaps and so their snap types can be ignored
    'service-control',
    'quota-control'
}


def _keys_in_dict(dictionary: dict[str, Any], *args) -> bool:
    '''
    Checks if the keys passed as args are in the dictionary,
    each nested inside the other. The first arg is the outermost
    key. So _keys_in_dict(d, 'first', 'second') will check if
    'first' in d and 'second' in d['first']

    :return: True if all keys are present, nested one inside the other.
    '''
    current_entry = dictionary
    for arg in args:
        if arg not in current_entry:
            return False
        current_entry = current_entry[arg]
    return True


class NotInStateError(Exception):
    pass


class State:

    def __init__(self, state_json: dict[str, Any]):
        self.state = state_json

    def get_change(self, id: str) -> dict[str, Any]:
        try:
            return self.state[CHANGES][id]
        except KeyError:
            raise NotInStateError(
                'change {} not found in state.json'.format(id))

    def get_task(self, id: str) -> dict[str, Any]:
        try:
            return self.state[TASKS][id]
        except KeyError:
            raise NotInStateError('task {} not found in state.json'.format(id))

    def get_snap_type(self, snap_name: str) -> str:
        '''
        Given a snap name, returns the type of snap. If the snap name is not present,
        returns the string "NOT_FOUND: " followed by the snap name
        '''
        if _keys_in_dict(self.state, DATA, SNAPS, snap_name, TYPE):
            return self.state[DATA][SNAPS][snap_name][TYPE]
        for task in self.state[TASKS].values():
            if _keys_in_dict(task, DATA, SNAP_SETUP, SIDE_INFO, NAME) \
                    and task[DATA][SNAP_SETUP][SIDE_INFO][NAME] == snap_name \
                    and _keys_in_dict(task, DATA, SNAP_SETUP, TYPE):
                return task[DATA][SNAP_SETUP][TYPE]

        return "NOT_FOUND: {}".format(snap_name)

    def get_snap_types_from_task_id(self, id: str) -> list[str]:
        '''
        Retrieves the type of snap associated with the task with the given id.
        If the task kind is present in the exception list or if the task does
        not have a data section, then returns an empty set.

        :raises KeyError: when the snap type was not found yet the kind of task is not in the exception list
        '''

        task = self.get_task(id)
        if task['kind'] in TASKS_IGNORE_SNAP_TYPE:
            return []
        if DATA not in task:
            return []
        data = task[DATA]

        if _keys_in_dict(data, SNAP_TYPE):
            return [data[SNAP_TYPE]]

        elif _keys_in_dict(data, SNAP_SETUP, TYPE):
            return [data[SNAP_SETUP][TYPE]]

        elif _keys_in_dict(data, SNAP_SETUP, SIDE_INFO, NAME):
            return [self.get_snap_type(data[SNAP_SETUP][SIDE_INFO][NAME])]

        elif _keys_in_dict(data, SNAP_SETUP_TASK):
            return self.get_snap_types_from_task_id(data[SNAP_SETUP_TASK])

        elif _keys_in_dict(data, HOOK_SETUP, SNAP):
            return [self.get_snap_type(data[HOOK_SETUP][SNAP])]

        elif _keys_in_dict(data, PLUG, SNAP) and _keys_in_dict(data, SLOT, SNAP):
            return [self.get_snap_type(data[PLUG][SNAP]), self.get_snap_type(data[SLOT][SNAP])]

        elif _keys_in_dict(data, SNAPS):  # ex: conditional-auto-refresh
            return list({snap_data[TYPE] for snap_data in data[SNAPS].values()})

        elif _keys_in_dict(data, SNAPSHOT_SETUP, SNAP):
            return [self.get_snap_type(data[SNAPSHOT_SETUP][SNAP])]

        elif _keys_in_dict(data, RECOVERY_SYSTEM_SETUP_TASK):
            return self.get_snap_types_from_task_id(data[RECOVERY_SYSTEM_SETUP_TASK])

        elif _keys_in_dict(data, RECOVERY_SYSTEM_SETUP, SNAP_SETUP_TASKS):
            return list({snap_type for setup_task in data[RECOVERY_SYSTEM_SETUP][SNAP_SETUP_TASKS] for snap_type in self.get_snap_types_from_task_id(setup_task)})

        raise NotInStateError(
            'cannot find snap type for task id {} in task {}'.format(id, task))

    def get_snap_types_from_change_id(self, id: str) -> set[str]:
        change = self.get_change(id)
        if 'task-ids' not in change:
            return set()
        tasks = change['task-ids']
        snap_types = set()
        for task in tasks:
            snap_types.update(self.get_snap_types_from_task_id(task))
        return snap_types
