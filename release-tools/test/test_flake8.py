import os
import subprocess
import unittest


def is_24_04():
    with open("/etc/os-release") as inf:
        return "VERSION_ID=\"24.04\"" in inf.read()


class TestFlake8(unittest.TestCase):
    @unittest.skipIf(is_24_04(), "flake8 is broken on 24.04")
    def test_flake8(self):
        p = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..")
        subprocess.check_call(["flake8", "--ignore=E501", p])
