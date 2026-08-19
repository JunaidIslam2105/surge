#!/usr/bin/env python3
"""Compare cached and invalidated TUI performance budget reports."""

from __future__ import annotations

import argparse
import re
import statistics
from pathlib import Path
from typing import Dict, List, Tuple

TIME_RE = re.compile(r"(cached|invalidated) frame: ([0-9.]+)(ns|µs|ms)/op")
ALLOCS_RE = re.compile(r"(cached|invalidated) frame: ([0-9.]+) allocs/op")
TIME_SCALE_MS = {"ns": 1e-6, "µs": 1e-3, "ms": 1.0}
MODES = ("cached", "invalidated")
TIME_RATIO_LIMIT = 1.25
ALLOCS_RATIO_LIMIT = 1.10


def parse_report(text: str) -> Tuple[Dict[str, List[float]], Dict[str, List[float]]]:
    """Return per-mode latency in milliseconds and allocation samples."""
    times = {mode: [] for mode in MODES}
    allocations = {mode: [] for mode in MODES}

    for mode, value, unit in TIME_RE.findall(text):
        times[mode].append(float(value) * TIME_SCALE_MS[unit])
    for mode, value in ALLOCS_RE.findall(text):
        allocations[mode].append(float(value))

    return times, allocations


def compare_reports(current_text: str, previous_text: str) -> Tuple[str, List[str]]:
    """Return a human-readable comparison and any budget violations."""
    current_times, current_allocations = parse_report(current_text)
    previous_times, previous_allocations = parse_report(previous_text)
    failures: List[str] = []
    lines: List[str] = []

    for mode in MODES:
        if not current_times[mode] or not previous_times[mode]:
            raise ValueError(f"missing {mode} latency samples")
        if not current_allocations[mode] or not previous_allocations[mode]:
            raise ValueError(f"missing {mode} allocation samples")

        current_time = statistics.median(current_times[mode])
        previous_time = statistics.median(previous_times[mode])
        current_allocs = statistics.median(current_allocations[mode])
        previous_allocs = statistics.median(previous_allocations[mode])
        time_delta = (current_time / previous_time - 1) * 100
        alloc_delta = (current_allocs / previous_allocs - 1) * 100

        lines.append(
            f"{mode}: {current_time:.3f} ms/op ({time_delta:+.1f}%), "
            f"{current_allocs:.0f} allocs/op ({alloc_delta:+.1f}%)"
        )
        if current_time > previous_time * TIME_RATIO_LIMIT:
            failures.append(f"{mode} latency exceeds {TIME_RATIO_LIMIT:.2f}x baseline")
        if current_allocs > previous_allocs * ALLOCS_RATIO_LIMIT:
            failures.append(f"{mode} allocations exceed {ALLOCS_RATIO_LIMIT:.2f}x baseline")

    lines.append(
        "FAIL: " + "; ".join(failures)
        if failures
        else "PASS: no material TUI performance regression detected"
    )
    return "\n".join(lines) + "\n", failures


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--current", required=True, type=Path)
    parser.add_argument("--previous", required=True, type=Path)
    parser.add_argument("--output", type=Path, default=Path("perf-comparison.txt"))
    args = parser.parse_args()

    try:
        report, failures = compare_reports(
            args.current.read_text(encoding="utf-8"),
            args.previous.read_text(encoding="utf-8"),
        )
    except (OSError, ValueError) as exc:
        report = f"FAIL: unable to compare TUI performance reports: {exc}\n"
        failures = [str(exc)]

    args.output.write_text(report, encoding="utf-8")
    print(report, end="")
    return 1 if failures else 0


if __name__ == "__main__":
    raise SystemExit(main())
