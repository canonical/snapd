import unittest
from io import StringIO
from traces_diff import LockOpTraceFileReader


class TestLockOpTraceFileReader(unittest.TestCase):
    def setUp(self):
        self.sample_trace_1 = """---TRACE 1---\n\nlock A\nunlock A\n"""
        self.sample_trace_2 = """---TRACE 2---\n\nlock B\nunlock B\n"""
        self.sample_trace_3 = """---TRACE 3---\n\nlock A\nunlock A\n"""

    def test_trace_diff(self):
        reader1 = LockOpTraceFileReader(StringIO(self.sample_trace_1))
        reader2 = LockOpTraceFileReader(StringIO(self.sample_trace_2))
        self.assertNotIn(reader2.traces[0], reader1.traces)

    def test_trace_no_diff(self):
        reader1 = LockOpTraceFileReader(StringIO(self.sample_trace_1))
        reader2 = LockOpTraceFileReader(StringIO(self.sample_trace_3))
        self.assertIn(reader2.traces[0], reader1.traces)


if __name__ == "__main__":
    unittest.main()
