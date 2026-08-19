#!/usr/bin/env python3

import unittest
from typing import Final

from compare_tui_perf import compare_reports, parse_report


REPORT: Final[str] = """\
    perf_budget_test.go:30: cached frame: 550 allocs/op (budget 850)
    perf_budget_test.go:41: cached frame: 0.250ms/op (budget 2ms)
    perf_budget_test.go:64: invalidated frame: 1500 allocs/op (budget 2200)
    perf_budget_test.go:77: invalidated frame: 0.750ms/op (budget 3ms)
"""


class CompareTUIPerfTests(unittest.TestCase):
    def test_parse_report_converts_units(self):
        times, allocations = parse_report(REPORT)
        self.assertEqual(times["cached"], [0.25])
        self.assertEqual(times["invalidated"], [0.75])
        self.assertEqual(allocations["cached"], [550.0])
        self.assertEqual(allocations["invalidated"], [1500.0])

    def test_small_change_passes(self):
        report, failures = compare_reports(REPORT, REPORT)
        self.assertIn("PASS", report)
        self.assertEqual(failures, [])

    def test_latency_regression_fails(self):
        slower = REPORT.replace("0.250ms", "0.400ms")
        report, failures = compare_reports(slower, REPORT)
        self.assertIn("cached latency exceeds", report)
        self.assertTrue(failures)

    def test_allocation_regression_fails(self):
        more_allocations = REPORT.replace("550 allocs", "700 allocs")
        report, failures = compare_reports(more_allocations, REPORT)
        self.assertIn("cached allocations exceed", report)
        self.assertTrue(failures)

    def test_missing_mode_is_rejected(self):
        with self.assertRaises(ValueError):
            compare_reports(REPORT.replace("invalidated frame:", "other frame:"), REPORT)


if __name__ == "__main__":
    unittest.main()
