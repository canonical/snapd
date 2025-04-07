import unittest
from io import StringIO

from common import LockOpTrace, get_next_match

from filter import LockOp, LocksGroup, GroupTimes, LockTraceManager, LocksFileReader


class TestLockOp(unittest.TestCase):
    def setUp(self):
        self.lines = ["header held: 10 ms wait 5 ms\n", "line 1\n", "line 2\n"]
        self.lock_op = LockOp(self.lines)

    def test_get_held_time(self):
        self.assertEqual(self.lock_op.get_held_time(), 10)

    def test_get_wait_time(self):
        self.assertEqual(self.lock_op.get_wait_time(), 5)

    def test_get_trace(self):
        expected_trace = LockOpTrace(["line 1\n", "line 2\n"])
        self.assertEqual(self.lock_op.get_trace(), expected_trace)


class TestLocksGroup(unittest.TestCase):
    def setUp(self):
        self.lines = [
            "###START: Test Group\n",
            "### Lock 1 header: held: 10 ms wait 5 ms\n",
            "line 1\n",
            "line 2\n",
            "### Lock 1 header: held: 20 ms wait 10 ms\n",
            "line 3\n",
        ]
        self.group = LocksGroup(self.lines)

    def test_get_name(self):
        self.assertEqual(self.group.get_name(), "###START: Test Group\n")

    def test_get_locks(self):
        self.assertEqual(len(self.group.get_locks()), 2)

    def test_get_traces(self):
        self.assertEqual(len(self.group.get_traces()), 2)

    def test_get_lock_held_time(self):
        trace = self.group.get_traces()[0]
        self.assertEqual(self.group.get_lock_held_time(trace), 10)

    def test_get_lock_wait_time(self):
        trace = self.group.get_traces()[0]
        self.assertEqual(self.group.get_lock_wait_time(trace), 5)


class TestGroupTimes(unittest.TestCase):
    def setUp(self):
        self.group_time = GroupTimes("Test Group", 10, 5)

    def test_get_group_name(self):
        self.assertEqual(self.group_time.get_group_name(), "Test Group")

    def test_get_held_time(self):
        self.assertEqual(self.group_time.get_held_time(), 10)

    def test_get_wait_time(self):
        self.assertEqual(self.group_time.get_wait_time(), 5)


class TestLockTraceManager(unittest.TestCase):
    def setUp(self):
        trace = LockOpTrace(["line 1\n", "line 2\n"])
        group_time = GroupTimes("Test Group", 10, 5)
        self.manager = LockTraceManager({trace: [group_time]})

    def test_filter(self):
        self.manager.filter(held_time=5, wait_time=3)
        self.assertEqual(len(self.manager.traces), 1)
        self.manager.filter(held_time=15, wait_time=3)
        self.assertEqual(len(self.manager.traces), 0)

    def test_match(self):
        self.manager.match(["line 1"])
        self.assertEqual(len(self.manager.traces), 1)
        self.manager.match(["line 3"])
        self.assertEqual(len(self.manager.traces), 0)


class TestLocksFileReader(unittest.TestCase):
    def setUp(self):
        self.file_content = (
            "###START: SNAPD PROJECT\n"
            "### Lock 1 header: held: 0 ms wait 50 ms\n"
            "line 1\n"
            "### Lock 2 header: held: 100 ms wait 0 ms\n"
            "line 2\n"
            "###START: Test 1\n"
            "### Lock 3 header: held: 10 ms wait 5 ms\n"
            "line 3\n"
            "line 4\n"
            "line 5\n"
        )
        self.file = StringIO(self.file_content)
        self.reader = LocksFileReader(self.file)

    def test_get_test(self):
        test_output = self.reader.get_test("Test 1")
        self.assertIn("###START: Test 1\n", test_output)

    def test_get_traces_times(self):
        traces_times = self.reader.get_traces_times()
        self.assertEqual(len(traces_times), 3)


class TestGetNextMatch(unittest.TestCase):
    def test_get_next_match(self):
        lines = ["line 1\n", "line 2\n", "###START: Test\n", "line 3\n"]
        result = get_next_match(lines, 0, "###START:")
        self.assertEqual(result, 2)
        result = get_next_match(lines, 2, "###START:")
        self.assertEqual(result, -1)


if __name__ == "__main__":
    unittest.main()
