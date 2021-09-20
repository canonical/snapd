import os
import os.path
import tempfile

import pytest


@pytest.fixture(scope="session")
def snapd_executables_dir(request):
    """ Location of the executable binaries.
    """
    return os.environ.get('SNAPD_TEST_BINARIES', '/tmp/snapd-test-binaries/')


@pytest.fixture(scope="session")
def coverage_dir(request):
    """ Directory where coverage data should be written to.
    """
    path = os.environ.get('SNAPD_COVERAGE_DIR', '/tmp/snapd-coverage-dir/')
    os.makedirs(path, exist_ok=True)
    return path


@pytest.fixture(scope="session")
def make_command(request, snapd_executables_dir, coverage_dir):
    """ Command to be run in order to run the `snap` executable.

    The return value is an array consisting of the program
    arguments in the same form expected by `subprocess.Popen()`.

    In the simple case this would just be a single-element tuple
    like `["snap"]`, but in practice we will want to have wrappers
    that help us setup the testing environment.
    """
    def _make_command(name, *args):
        fd, coverage_file_path = tempfile.mkstemp('.cov', name, coverage_dir)
        print('Coverage file path: {}'.format(coverage_file_path))
        os.close(fd)

        return [
            os.path.join(snapd_executables_dir, name),
            '-test.coverprofile={}'.format(coverage_file_path),
            *args
        ]

    return _make_command
