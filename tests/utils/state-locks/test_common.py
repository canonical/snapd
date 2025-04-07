import unittest

from common import (
    LockOpTrace,
)


class TestLockOpTrace(unittest.TestCase):
    def setUp(self):
        self.lines = ["line 1\n", "line 2\n", "line 3\n"]
        self.trace = LockOpTrace(self.lines)

    def test_get_trace_lines(self):
        self.assertEqual(self.trace.get_trace_lines(), self.lines)

    def test_match(self):
        self.assertTrue(self.trace.match("line 2"))
        self.assertFalse(self.trace.match("line 4"))

    def test_str(self):
        self.assertEqual(str(self.trace), "line 1\nline 2\nline 3")

    def test_eq(self):
        other_trace = LockOpTrace(self.lines)
        self.assertEqual(self.trace, other_trace)
