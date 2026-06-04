snapd.spread-tests-install-mode-tweaks#!/usr/bin/env python3

import argparse
import json
import os
import re
import subprocess
import sys
import tempfile
from pathlib import Path


# data looks like <path>:<start-line>.<start-col>,<end-line>.<end-col> <num-statements> <count>
PROFILE_LINE_RE = re.compile(r"^(.*):([0-9]+)\.[0-9]+,([0-9]+)\.[0-9]+\s+([0-9]+)\s+([0-9]+)$")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        prog=os.path.basename(sys.argv[0]),
        description="Must be run from snapd code directory",
    )
    parser.add_argument("-results-dir", required=True, help="path to directory containing raw coverage data")
    parser.add_argument("-output", default="files", choices=["files", "functions"], help="output mode: files|functions")
    return parser.parse_args()


def ensure_dir(path: Path) -> None:
    if not path.is_dir():
        raise ValueError(f"{path} is not a directory")


def read_module_path() -> str:
    try:
        content = Path("go.mod").read_text(encoding="utf-8")
    except OSError as exc:
        raise RuntimeError("could not find go.mod; utility must be called from snapd code directory") from exc

    for line in content.splitlines():
        line = line.strip()
        if line.startswith("module "):
            module_path = line.removeprefix("module ").strip()
            if module_path:
                return module_path

    raise RuntimeError("module path not found in go.mod")


def run_covdata_textfmt(covdata_dir: Path) -> Path:
    fd, tmp_path = tempfile.mkstemp(prefix="snapd-cov-profile-", suffix=".out")
    os.close(fd)
    out_path = Path(tmp_path)

    cmd = ["go", "tool", "covdata", "textfmt", "-i", str(covdata_dir), "-o", str(out_path)]
    proc = subprocess.run(cmd, check=False, capture_output=True, text=True)
    if proc.returncode == 0:
        return out_path

    # Fall back to converting each pod separately and merge successful outputs.
    pod_dirs = sorted(path for path in covdata_dir.iterdir() if path.is_dir())
    if not pod_dirs:
        try:
            out_path.unlink()
        except OSError:
            pass
        details = (proc.stdout + "\n" + proc.stderr).strip()
        raise RuntimeError(f"cannot convert raw coverage: exit code {proc.returncode} ({details})")

    mode_line = None
    merged_lines: list[str] = []
    pod_errors: list[str] = []

    for pod_dir in pod_dirs:
        pod_fd, pod_tmp_path = tempfile.mkstemp(prefix="snapd-cov-profile-pod-", suffix=".out")
        os.close(pod_fd)
        pod_out_path = Path(pod_tmp_path)

        pod_cmd = ["go", "tool", "covdata", "textfmt", "-i", str(pod_dir), "-o", str(pod_out_path)]
        pod_proc = subprocess.run(pod_cmd, check=False, capture_output=True, text=True)
        if pod_proc.returncode != 0:
            details = (pod_proc.stdout + "\n" + pod_proc.stderr).strip()
            pod_errors.append(f"{pod_dir.name}: exit code {pod_proc.returncode} ({details})")
            try:
                pod_out_path.unlink()
            except OSError:
                pass
            continue

        try:
            with pod_out_path.open(encoding="utf-8") as f:
                for raw_line in f:
                    line = raw_line.rstrip("\n")
                    if not line:
                        continue
                    if line.startswith("mode:"):
                        if mode_line is None:
                            mode_line = line
                        continue
                    merged_lines.append(line)
        finally:
            try:
                pod_out_path.unlink()
            except OSError:
                pass

    if not merged_lines:
        try:
            out_path.unlink()
        except OSError:
            pass

        details = (proc.stdout + "\n" + proc.stderr).strip()
        if pod_errors:
            details = f"{details}; per-pod fallback also failed: {'; '.join(pod_errors)}"
        raise RuntimeError(f"cannot convert raw coverage: exit code {proc.returncode} ({details})")

    with out_path.open("w", encoding="utf-8") as out_file:
        out_file.write((mode_line or "mode: set") + "\n")
        out_file.write("\n".join(merged_lines) + "\n")

    return out_path


def normalize_profile_path(path: str, module_path: str) -> str:
    clean = path.strip().replace("\\", "/")
    module_prefix = module_path.rstrip("/") + "/"
    if clean.startswith(module_prefix):
        clean = clean[len(module_prefix) :]
    if clean.startswith("./"):
        clean = clean[2:]

    clean = os.path.normpath(clean).replace("\\", "/")
    if clean == "." or clean.startswith("../"):
        return ""
    return clean


def parse_profile(profile_path: Path, module_path: str) -> dict[str, set[int]]:
    result: dict[str, set[int]] = {}

    with profile_path.open(encoding="utf-8") as f:
        for raw_line in f:
            line = raw_line.strip()
            if not line or line.startswith("mode:"):
                continue

            match = PROFILE_LINE_RE.match(line)
            if not match:
                continue

            path = normalize_profile_path(match.group(1), module_path)
            if not path:
                continue

            try:
                start_line = int(match.group(2))
                end_line = int(match.group(3))
                num_statements = int(match.group(4))
                count = int(match.group(5))
            except ValueError:
                continue

            if end_line < start_line:
                start_line, end_line = end_line, start_line

            if path not in result:
                result[path] = set()
            if count > 0 and num_statements > 0:
                result[path].update(range(start_line, end_line + 1))

    return result


def profile_with_existing_files(profile_path: Path, module_path: str) -> Path:
    fd, tmp_path = tempfile.mkstemp(prefix="snapd-cov-profile-existing-", suffix=".out")
    os.close(fd)
    out_path = Path(tmp_path)

    with profile_path.open(encoding="utf-8") as src, out_path.open("w", encoding="utf-8") as dst:
        for raw_line in src:
            line = raw_line.strip()
            if not line:
                continue

            if line.startswith("mode:"):
                dst.write(raw_line if raw_line.endswith("\n") else raw_line + "\n")
                continue

            match = PROFILE_LINE_RE.match(line)
            if not match:
                # Preserve unexpected lines so go tool cover can fail normally.
                dst.write(raw_line if raw_line.endswith("\n") else raw_line + "\n")
                continue

            normalized = normalize_profile_path(match.group(1), module_path)
            if normalized and not Path(normalized).is_file():
                continue

            dst.write(raw_line if raw_line.endswith("\n") else raw_line + "\n")

    return out_path


def covered_functions_by_file(profile_path: Path, module_path: str) -> dict[str, set[str]]:
    filtered_profile = profile_with_existing_files(profile_path, module_path)

    try:
        cmd = ["go", "tool", "cover", f"-func={filtered_profile}"]
        proc = subprocess.run(cmd, check=False, capture_output=True, text=True)
        if proc.returncode != 0:
            details = (proc.stdout + "\n" + proc.stderr).strip()
            raise RuntimeError(f"cannot extract covered functions: exit code {proc.returncode} ({details})")

        result: dict[str, set[str]] = {}
        for raw_line in proc.stdout.splitlines():
            line = raw_line.strip()
            if not line or line.startswith("total:"):
                continue

            fields = line.split()
            if len(fields) < 3:
                continue

            location = fields[0]
            func_name = fields[1]
            percent = fields[-1]
            if not percent.endswith("%"):
                continue

            try:
                covered_percent = float(percent[:-1])
            except ValueError:
                continue
            if covered_percent <= 0:
                continue

            if not location.endswith(":"):
                continue
            file_path_and_line = location[:-1]
            if ":" not in file_path_and_line:
                continue
            file_path, _line = file_path_and_line.rsplit(":", 1)

            normalized = normalize_profile_path(file_path, module_path)
            if not normalized:
                continue

            if normalized not in result:
                result[normalized] = set()
            result[normalized].add(func_name)

        return result
    finally:
        try:
            filtered_profile.unlink()
        except OSError:
            pass


def print_covered_files(coverage: dict[str, set[int]]) -> None:
    covered_files = sorted(path for path, lines in coverage.items() if lines)
    for path in covered_files:
        print(path)


def print_functions_json(coverage: dict[str, set[int]], by_file_functions: dict[str, set[str]]) -> None:
    paths = sorted(path for path, lines in coverage.items() if lines)
    payload = {"files": []}

    for path in paths:
        payload["files"].append(
            {
                "path": path,
                "covered_functions": sorted(by_file_functions.get(path, set())),
            }
        )

    json.dump(payload, sys.stdout, indent=2)
    sys.stdout.write("\n")


def main() -> int:
    args = parse_args()

    try:
        results_dir = Path(args.results_dir).resolve()
        ensure_dir(results_dir)
        module_path = read_module_path()
        profile_path = run_covdata_textfmt(results_dir)
        try:
            coverage = parse_profile(profile_path, module_path)
            if args.output == "files":
                print_covered_files(coverage)
            else:
                by_file_functions = covered_functions_by_file(profile_path, module_path)
                print_functions_json(coverage, by_file_functions)
        finally:
            try:
                profile_path.unlink()
            except OSError:
                pass
    except Exception as exc:  # pylint: disable=broad-except
        print(f"cannot continue: {exc}", file=sys.stderr)
        return 1

    return 0


if __name__ == "__main__":
    raise SystemExit(main())