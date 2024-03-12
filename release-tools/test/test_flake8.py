import os
import subprocess
import unittest


class TestFlake8(unittest.TestCase):
    def test_flake8(self):
        p = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..")
        subprocess.check_call(["flake8", "--ignore=E501", p])
