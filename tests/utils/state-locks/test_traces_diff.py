import unittest
from io import StringIO
from traces_diff import LockOpTraceFileReader


class TestLockOpTraceFileReader(unittest.TestCase):
    def setUp(self):
        self.baseline_trace = """---TRACE 1---\n\nfile1:123 lock A\nfile1:234 unlock A\n"""
        # Diff
        self.sample_trace_1 = """---TRACE 2---\n\nfile2:123 lock B\nfile1:123 unlock A\n"""
        self.sample_trace_2 = """---TRACE 2---\n\nfile1:123 lock B\nfile1:234 unlock A\n"""
        # No Diff
        self.sample_trace_3 = """---TRACE 3---\n\nfile1:123 lock A\nfile1:234 unlock A\n"""
        self.sample_trace_4 = """---TRACE 4---\n\nfile1:123 lock A\nfile1:235 unlock A\n"""
        self.sample_trace_5 = """---TRACE 5---\n\nfile2:123 lock A\nfile1:234 unlock A\n"""

    def test_trace_diff_1(self):
        reader1 = LockOpTraceFileReader(StringIO(self.baseline_trace))
        reader2 = LockOpTraceFileReader(StringIO(self.sample_trace_1))
        self.assertIsNot(reader1.get_diff(reader2), [])

    def test_trace_diff_2(self):
        reader1 = LockOpTraceFileReader(StringIO(self.baseline_trace))
        reader2 = LockOpTraceFileReader(StringIO(self.sample_trace_2))
        self.assertIsNot(reader1.get_diff(reader2), [])

    def test_trace_no_diff_1(self):
        reader1 = LockOpTraceFileReader(StringIO(self.baseline_trace))
        reader2 = LockOpTraceFileReader(StringIO(self.sample_trace_3))
        self.assertIs(len(reader1.get_diff(reader2)), 0)

    def test_trace_no_diff_2(self):
        reader1 = LockOpTraceFileReader(StringIO(self.baseline_trace))
        reader2 = LockOpTraceFileReader(StringIO(self.sample_trace_4))
        self.assertIs(len(reader1.get_diff(reader2)), 0)

    def test_trace_no_diff_3(self):
        reader1 = LockOpTraceFileReader(StringIO(self.baseline_trace))
        reader2 = LockOpTraceFileReader(StringIO(self.sample_trace_5))
        self.assertIs(len(reader1.get_diff(reader2)), 0)


if __name__ == "__main__":
    unittest.main()
