import subprocess

import pytest


class TestParams:
    @pytest.mark.parametrize('params, expected_error', [
        pytest.param(['snap', 'does-not-exists'],
                     (b'error: unknown command "does-not-exists", '
                      b"see 'snap help'.")),
        pytest.param(['snap', 'abort', 'one', 'two'],
                     b'error: too many arguments for command'),
        pytest.param(['snap', 'ack', 'one', 'two'],
                     b'error: too many arguments for command'),
        pytest.param(['snap', 'advise-snap', 'one', 'two'],
                     b'error: too many arguments for command'),
        pytest.param(['snap', 'advise-snap'],
                     (b'error: the required argument '
                      b'`<command or pkg>` was not provided')),
        pytest.param(['snap', 'alias', 'one', 'two', 'three'],
                     b'error: too many arguments for command'),
        pytest.param(['snap', 'aliases', 'one', 'two'],
                     b'error: too many arguments for command'),
        pytest.param(['snap', 'auto-import', 'one', 'two'],
                     b'error: too many arguments for command'),
        pytest.param(['snap', 'booted', 'one'],
                     b'error: too many arguments for command'),
        pytest.param(['snap', 'booted'],
                     b'booted command is deprecated'),
        pytest.param(['snap', 'buy', 'one', 'two'],
                     b'error: too many arguments for command'),
        pytest.param(['snap', 'can-manage-refreshes'],
                     (b'error: unknown command "can-manage-refreshes", '
                      b"see 'snap help'.")),
        pytest.param(['snap', 'debug', 'can-manage-refreshes', 'one'],
                     b'error: too many arguments for command'),
        pytest.param(['snap', 'changes', 'one', 'two'],
                     b'error: too many arguments for command'),
        pytest.param(['snap', 'changes', '123'],
                     (b"error: 'snap changes' command expects a snap name, "
                      b"try 'snap tasks 123'")),
        pytest.param(['snap', 'changes', 'everything'], b''),
        pytest.param(['snap', 'tasks'],
                     (b'error: please provide change ID or '
                      b'type with --last=<type>')),
    ])
    def test_invalid_params(self, make_command, params, expected_error):
        command = make_command(*params)
        process = subprocess.run(command,
                                 stderr=subprocess.PIPE)
        assert process.stderr.strip() == expected_error
