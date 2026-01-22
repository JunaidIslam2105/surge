#!/usr/bin/env python3
"""
Bayesian Optimization Tuning script for Surge downloader.
Uses Optuna to find optimal constants by maximizing throughput.
"""

import os
import re
import sys
import shutil
import subprocess
import argparse
import optuna
from pathlib import Path
from typing import Dict, List, Any

# =============================================================================
# CONFIGURATION
# =============================================================================

# We use the same exact strings to ensure Go syntax validity (KB/MB)
SEARCH_SPACE = {
    "MinChunk":     ["512 * KB", "1 * MB", "2 * MB", "4 * MB"],
    "MaxChunk":     ["8 * MB", "16 * MB", "32 * MB", "64 * MB"],
    "TargetChunk":  ["4 * MB", "8 * MB", "16 * MB", "32 * MB"],
    "WorkerBuffer": ["32 * KB", "64 * KB", "128 * KB", "256 * KB", "512 * KB"],
    "TasksPerWorker": ["2", "4", "8", "16"],
    "PerHostMax":   ["8", "16", "32", "64", "128"],
}

# Mapping to regex patterns in config.go
REGEX_MAP = {
    "MinChunk":     r"(MinChunk\s*=\s*)(.*)(  // Minimum chunk size)",
    "MaxChunk":     r"(MaxChunk\s*=\s*)(.*)( // Maximum chunk size)",
    "TargetChunk":  r"(TargetChunk\s*=\s*)(.*)(  // Target chunk size)",
    "WorkerBuffer": r"(WorkerBuffer\s*=\s*)(.*)",
    "TasksPerWorker": r"(TasksPerWorker\s*=\s*)(.*)( // Target tasks per connection)",
    "PerHostMax":   r"(PerHostMax\s*=\s*)(.*)( // Max concurrent connections per host)",
}

CONFIG_FILE = Path("internal/download/types/config.go").resolve()
BENCHMARK_SCRIPT = Path("benchmark.py").resolve()
PROJECT_ROOT = Path(__file__).parent.parent.resolve()

# =============================================================================
# UTILS
# =============================================================================

def run_command(cmd: List[str], cwd: Path = PROJECT_ROOT, timeout: int = 600) -> tuple[bool, str]:
    try:
        result = subprocess.run(
            cmd, cwd=str(cwd), capture_output=True, text=True, timeout=timeout
        )
        return result.returncode == 0, result.stdout + result.stderr
    except Exception as e:
        return False, str(e)

def backup_config():
    if CONFIG_FILE.exists():
        shutil.copy(CONFIG_FILE, str(CONFIG_FILE) + ".bak")

def restore_config():
    bak = Path(str(CONFIG_FILE) + ".bak")
    if bak.exists():
        shutil.copy(bak, CONFIG_FILE)
        bak.unlink()

def apply_config(params: Dict[str, str]):
    """Reads config.go, regex replaces values based on params dict."""
    if not CONFIG_FILE.exists():
        raise FileNotFoundError(f"Config file not found: {CONFIG_FILE}")

    content = CONFIG_FILE.read_text()
    
    for key, val in params.items():
        pattern = REGEX_MAP.get(key)
        if not pattern:
            continue

        # Special check for WorkerBuffer to preserve potentially missing comment group
        if key == "WorkerBuffer":
             content = re.sub(r"(WorkerBuffer\s*=\s*)(.*)", f"\\g<1>{val}", content)
        else:
             content = re.sub(pattern, f"\\g<1>{val}\\g<3>", content)

    CONFIG_FILE.write_text(content)

def compile_surge() -> bool:
    # Use quiet build to keep logs clean
    cmd = ["go", "build", "-o", "surge-tuned", "."]
    success, out = run_command(cmd)
    return success

def run_benchmark(iterations: int) -> float:
    """Runs benchmark.py and returns average speed in MB/s."""
    cmd = [
        sys.executable,
        str(BENCHMARK_SCRIPT),
        "--surge-exec", str(PROJECT_ROOT / "surge-tuned"),
        "-n", str(iterations),
        "--surge"
    ]
    
    success, output = run_command(cmd)
    if not success:
        return 0.0

    # Parse output for average speed
    match = re.search(r"surge \(current\).*?│\s*([\d\.]+)\s*MB/s", output)
    if match:
        return float(match.group(1))
    return 0.0

# =============================================================================
# OPTUNA OBJECTIVE
# =============================================================================

def objective(trial):
    # 1. Suggest Parameters
    # We use suggest_categorical to pick from your exact string lists
    params = {
        key: trial.suggest_categorical(key, choices)
        for key, choices in SEARCH_SPACE.items()
    }

    # 2. Constraint Checks (Optional but recommended)
    # Parse sizes to allow comparison logic (e.g. MinChunk < MaxChunk)
    def parse_size(s):
        val = int(s.split(" * ")[0])
        unit = s.split(" * ")[1]
        return val * 1024 if unit == "MB" else val 

    if parse_size(params["MinChunk"]) > parse_size(params["TargetChunk"]):
        # Prune invalid configurations early without compiling
        raise optuna.TrialPruned("MinChunk > TargetChunk")

    # 3. Apply & Compile
    backup_config()
    try:
        apply_config(params)
        if not compile_surge():
            # If build fails, report 0 speed
            return 0.0
        
        # 4. Benchmark
        # We use fewer iterations for the search phase to save time,
        # but you can increase this via arguments if noise is high.
        speed = run_benchmark(iterations=3)
        
    finally:
        restore_config()

    return speed

# =============================================================================
# MAIN
# =============================================================================

def main():
    parser = argparse.ArgumentParser(description="Tune Surge using Bayesian Optimization")
    parser.add_argument("--trials", type=int, default=50, help="Number of trials to run")
    parser.add_argument("--db", type=str, default="sqlite:///surge_opt.db", help="Database for resume capability")
    args = parser.parse_args()

    os.chdir(PROJECT_ROOT)

    print(f"--- Starting Optuna Optimization ({args.trials} trials) ---")
    
    # We use a persistent storage (SQLite) so you can stop/resume the script
    study = optuna.create_study(
        study_name="surge_tuning",
        direction="maximize",
        storage=args.db,
        load_if_exists=True,
        sampler=optuna.samplers.TPESampler(seed=42) # Tree-structured Parzen Estimator
    )

    try:
        study.optimize(objective, n_trials=args.trials)
    except KeyboardInterrupt:
        print("\nOptimization interrupted by user.")
        restore_config()
    
    print("\n" + "="*50)
    print("Optimization Complete.")
    print(f"Best Speed: {study.best_value:.2f} MB/s")
    print("Best Params:")
    for k, v in study.best_params.items():
        print(f"  {k}: {v}")
    print("="*50)

    # Optional: Visualize importance (requires matplotlib/plotly)
    # print("Param Importance:", optuna.importance.get_param_importance(study))

if __name__ == "__main__":
    main()
