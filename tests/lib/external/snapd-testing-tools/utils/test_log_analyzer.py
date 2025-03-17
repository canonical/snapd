from importlib.util import spec_from_loader, module_from_spec
from importlib.machinery import SourceFileLoader
import os
import unittest

# Since log-analyzer has a hyphen and is missing the .py extension,
# we need to do some extra work to import the module to test
dir_path = os.path.dirname(os.path.realpath(__file__))
spec = spec_from_loader(
    "log-analyzer",
    SourceFileLoader("log-analyzer", os.path.join(dir_path, "log-analyzer")),
)
log_analyzer = module_from_spec(spec)
spec.loader.exec_module(log_analyzer)


def create_data(
    num_executed_no_fail,
    num_fail_execution,
    num_fail_restore,
    num_fail_prepare,
    num_not_executed,
):
    # The order will be:
    #   1. tasks that executed and didn't fail
    #   2. tasks that failed during execution
    #   3. tasks that failed during restore
    #   4. tasks that failed during prepare
    #   5. tasks that were not executed at all

    exec_param = [
        "test_" + str(i)
        for i in range(
            num_executed_no_fail
            + num_fail_execution
            + num_fail_prepare
            + num_fail_restore
            + num_not_executed
        )
    ]

    # The tasks that executed are those that didn't fail plus those that failed during execution or restore
    spread_logs = [
        {"type": "phase", "verb": "Executing", "task": param}
        for param in exec_param[
            : num_executed_no_fail + num_fail_execution + num_fail_restore
        ]
    ]

    begin = num_executed_no_fail
    end = num_executed_no_fail + num_fail_execution
    # The tasks that failed are those that failed during execution, not during restore or prepare
    spread_logs.append(
        {
            "type": "result",
            "result_type": "Failed",
            "level": "tasks",
            "detail": {"lines": ["- %s\n" % param for param in exec_param[begin:end]]},
        }
    )

    begin = num_executed_no_fail + num_fail_execution
    end = num_executed_no_fail + num_fail_execution + num_fail_restore
    # Tasks that failed during the restore phase
    spread_logs.append(
        {
            "type": "result",
            "result_type": "Failed",
            "level": "task",
            "stage": "restore",
            "detail": {"lines": ["- %s\n" % param for param in exec_param[begin:end]]},
        }
    )

    begin = num_executed_no_fail + num_fail_execution + num_fail_restore
    end = (
        num_executed_no_fail + num_fail_execution + num_fail_restore + num_fail_prepare
    )
    # Tasks that failed during the prepare phase
    spread_logs.append(
        {
            "type": "result",
            "result_type": "Failed",
            "level": "task",
            "stage": "prepare",
            "detail": {"lines": ["- %s\n" % param for param in exec_param[begin:end]]},
        }
    )

    return set(exec_param), spread_logs


class TestLogAnalyzer(unittest.TestCase):

    def __init__(self, *args, **kwargs):
        super(TestLogAnalyzer, self).__init__(*args, **kwargs)

        self.filtered_exec_param_mixed, self.spread_logs_mixed = create_data(
            num_executed_no_fail=10,
            num_fail_execution=10,
            num_fail_restore=10,
            num_fail_prepare=10,
            num_not_executed=10,
        )
        self.exec_param_mixed = "tests/..."

        self.filtered_exec_param_no_failed, self.spread_logs_no_failed = create_data(
            num_executed_no_fail=10,
            num_fail_execution=0,
            num_fail_restore=0,
            num_fail_prepare=0,
            num_not_executed=0,
        )
        self.exec_param_no_failed = "tests/..."

        self.filtered_exec_param_no_exec, self.spread_logs_no_exec = create_data(
            num_executed_no_fail=0,
            num_fail_execution=0,
            num_fail_restore=0,
            num_fail_prepare=10,
            num_not_executed=10,
        )
        self.exec_param_no_exec = "tests/..."

        (
            self.filtered_exec_param_mix_success_abort,
            self.spread_logs_mix_success_abort,
        ) = create_data(
            num_executed_no_fail=10,
            num_fail_execution=0,
            num_fail_restore=0,
            num_fail_prepare=0,
            num_not_executed=10,
        )
        self.exec_param_mix_success_abort = "tests/..."

    # The following test group has mixed results with task results
    # of all kinds: successful, failed in all three phases, and not run

    def test_list_executed__mixed(self):
        actual = log_analyzer.list_executed_tasks(
            self.filtered_exec_param_mixed, self.spread_logs_mixed
        )
        expected = set(["test_" + str(i) for i in range(30)])
        self.assertSetEqual(expected, actual)

    def test_list_failed__mixed(self):
        actual = log_analyzer.list_failed_tasks(
            self.filtered_exec_param_mixed, self.spread_logs_mixed
        )
        expected = set(["test_" + str(i) for i in range(10, 20)])
        self.assertSetEqual(expected, actual)

    def test_list_successful__mixed(self):
        actual = log_analyzer.list_successful_tasks(
            self.filtered_exec_param_mixed, self.spread_logs_mixed
        )
        expected = set(["test_" + str(i) for i in range(10)])
        self.assertSetEqual(expected, actual)

    def test_executed_and_failed__mixed(self):
        actual = log_analyzer.list_executed_and_failed(
            self.filtered_exec_param_mixed, self.spread_logs_mixed
        )
        expected = set(["test_" + str(i) for i in range(10, 30)])
        self.assertSetEqual(expected, actual)

    def test_aborted_tasks__mixed(self):
        actual = log_analyzer.list_aborted_tasks(
            self.filtered_exec_param_mixed, self.spread_logs_mixed
        )
        expected = set(["test_" + str(i) for i in range(30, 50)])
        self.assertSetEqual(expected, actual)

    def test_reexecute_tasks__mixed(self):
        actual = log_analyzer.list_rexecute_tasks(
            self.exec_param_mixed,
            self.filtered_exec_param_mixed,
            self.spread_logs_mixed,
        )
        expected = set(["test_" + str(i) for i in range(10, 50)])
        self.assertSetEqual(expected, actual)

    # The following test group has only tasks that were successfully run

    def test_list_executed__no_fail(self):
        actual = log_analyzer.list_executed_tasks(
            self.filtered_exec_param_no_failed, self.spread_logs_no_failed
        )
        expected = set(["test_" + str(i) for i in range(10)])
        self.assertSetEqual(expected, actual)

    def test_list_failed__no_fail(self):
        actual = log_analyzer.list_failed_tasks(
            self.filtered_exec_param_no_failed, self.spread_logs_no_failed
        )
        self.assertEqual(0, len(actual))

    def test_list_successful__no_fail(self):
        actual = log_analyzer.list_successful_tasks(
            self.filtered_exec_param_no_failed, self.spread_logs_no_failed
        )
        expected = set(["test_" + str(i) for i in range(10)])
        self.assertSetEqual(expected, actual)

    def test_executed_and_failed__no_fail(self):
        actual = log_analyzer.list_executed_and_failed(
            self.filtered_exec_param_no_failed, self.spread_logs_no_failed
        )
        self.assertEqual(0, len(actual))

    def test_aborted_tasks__no_fail(self):
        actual = log_analyzer.list_aborted_tasks(
            self.filtered_exec_param_no_failed, self.spread_logs_no_failed
        )
        self.assertEqual(0, len(actual))

    def test_reexecute_tasks__no_fail(self):
        actual = log_analyzer.list_rexecute_tasks(
            self.exec_param_no_failed,
            self.filtered_exec_param_no_failed,
            self.spread_logs_no_failed,
        )
        self.assertEqual(0, len(actual))

    # The following group only has tasks that either failed
    # during the prepare phase or were not run at all

    def test_list_executed__no_exec(self):
        actual = log_analyzer.list_executed_tasks(
            self.filtered_exec_param_no_exec, self.spread_logs_no_exec
        )
        self.assertEqual(0, len(actual))

    def test_list_failed__no_exec(self):
        actual = log_analyzer.list_failed_tasks(
            self.filtered_exec_param_no_exec, self.spread_logs_no_exec
        )
        self.assertEqual(0, len(actual))

    def test_list_successful__no_exec(self):
        actual = log_analyzer.list_successful_tasks(
            self.filtered_exec_param_no_exec, self.spread_logs_no_exec
        )
        self.assertEqual(0, len(actual))

    def test_executed_and_failed__no_exec(self):
        actual = log_analyzer.list_executed_and_failed(
            self.filtered_exec_param_no_exec, self.spread_logs_no_exec
        )
        self.assertEqual(0, len(actual))

    def test_aborted_tasks__no_exec(self):
        actual = log_analyzer.list_aborted_tasks(
            self.filtered_exec_param_no_exec, self.spread_logs_no_exec
        )
        expected = set(["test_" + str(i) for i in range(20)])
        self.assertSetEqual(expected, actual)

    def test_reexecute_tasks__no_exec(self):
        actual = log_analyzer.list_rexecute_tasks(
            self.exec_param_no_exec,
            self.filtered_exec_param_no_exec,
            self.spread_logs_no_exec,
        )
        self.assertSetEqual(set(["tests/..."]), actual)

    # The following test group has tasks that either
    # were successful or did not run at all

    def test_list_executed__mix_success_abort(self):
        actual = log_analyzer.list_executed_tasks(
            self.filtered_exec_param_mix_success_abort,
            self.spread_logs_mix_success_abort,
        )
        expected = set(["test_" + str(i) for i in range(10)])
        self.assertSetEqual(expected, actual)

    def test_list_failed__mix_success_abort(self):
        actual = log_analyzer.list_failed_tasks(
            self.filtered_exec_param_mix_success_abort,
            self.spread_logs_mix_success_abort,
        )
        self.assertEqual(len(actual), 0)

    def test_list_successful__mix_success_abort(self):
        actual = log_analyzer.list_successful_tasks(
            self.filtered_exec_param_mix_success_abort,
            self.spread_logs_mix_success_abort,
        )
        expected = set(["test_" + str(i) for i in range(10)])
        self.assertSetEqual(expected, actual)

    def test_executed_and_failed__mix_success_abort(self):
        actual = log_analyzer.list_executed_and_failed(
            self.filtered_exec_param_mix_success_abort,
            self.spread_logs_mix_success_abort,
        )
        self.assertEqual(len(actual), 0)

    def test_aborted_tasks__mix_success_abort(self):
        actual = log_analyzer.list_aborted_tasks(
            self.filtered_exec_param_mix_success_abort,
            self.spread_logs_mix_success_abort,
        )
        expected = set(["test_" + str(i) for i in range(10, 20)])
        self.assertSetEqual(expected, actual)

    def test_reexecute_tasks__mix_success_abort(self):
        actual = log_analyzer.list_rexecute_tasks(
            self.exec_param_mix_success_abort,
            self.filtered_exec_param_mix_success_abort,
            self.spread_logs_mix_success_abort,
        )
        expected = set(["test_" + str(i) for i in range(10, 20)])
        self.assertSetEqual(expected, actual)


if __name__ == "__main__":
    unittest.main()
