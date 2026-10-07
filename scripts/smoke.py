#!/usr/bin/env python3
"""Exercise compiled processes against fresh loopback-only fixtures."""

import json
import os
from pathlib import Path
import select
import shutil
import subprocess
import tempfile
import time
from urllib.request import build_opener, ProxyHandler


ROOT = Path(__file__).resolve().parents[1]
ENV = {**os.environ, "HOOKCHECK_DEMO_SECRET": "smoke-only-secret"}
LOOPBACK_HTTP = build_opener(ProxyHandler({}))


def exercise(mode, scenario, expected_exit, *, delay=None, hurl=False):
    args = [str(ROOT / "bin/payment-demo"), "--listen", "127.0.0.1:0", "--mode", mode]
    if delay:
        args += ["--ack-delay", delay]
    service = subprocess.Popen(args, env=ENV, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    try:
        if not select.select([service.stdout], [], [], 5)[0]:
            raise AssertionError("demo did not announce its address")
        line = service.stdout.readline().strip()
        if "url=http://127.0.0.1:" not in line:
            raise AssertionError(f"unexpected demo announcement: {line}")
        base = line.split("url=", 1)[1]
        with tempfile.TemporaryDirectory(prefix="hookcheck-smoke-") as directory:
            report_path = Path(directory) / "report.json"
            command = [str(ROOT / "bin/hookcheck"), "run", "--scenario", str(ROOT / scenario),
                       "--base-url", base, "--report", str(report_path)]
            if hurl:
                command.append("--allow-hurl")
            result = subprocess.run(command, env=ENV, capture_output=True, text=True, timeout=10)
            if result.returncode != expected_exit:
                raise AssertionError(f"exit={result.returncode}, expected={expected_exit}\n{result.stdout}\n{result.stderr}")
            report = json.loads(report_path.read_text())
            assert report["passed"] == (expected_exit == 0)
            assert report["attempts"], "missing delivery plan"
            assert report_path.stat().st_mode & 0o077 == 0, "report is not private"
            if delay:
                assert sum(a["outcome"] == "timeout" for a in report["attempts"]) == 1
                assert all(c["outcome"] == "passed" for c in report["checks"])
            else:
                assert all(a["outcome"] == "passed" for a in report["attempts"])
            serialized = report_path.read_text()
            assert ENV["HOOKCHECK_DEMO_SECRET"] not in serialized
            assert base not in serialized
            label = "hurl" if hurl else "native"
            print(f"{mode}/{label}/{Path(scenario).name}: exit {result.returncode}; "
                  f"{len(report['attempts'])} deliveries, {len(report['checks'])} checks")
            return report
    finally:
        service.terminate()
        try:
            service.communicate(timeout=5)
        except subprocess.TimeoutExpired:
            service.kill()
            service.communicate()


def interrupt_run():
    """A process signal must produce exit130 and a readable partial report."""
    args = [str(ROOT / "bin/payment-demo"), "--listen", "127.0.0.1:0", "--ack-delay", "10s"]
    service = subprocess.Popen(args, env=ENV, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    client = None
    try:
        assert select.select([service.stdout], [], [], 5)[0]
        base = service.stdout.readline().strip().split("url=", 1)[1]
        with tempfile.TemporaryDirectory(prefix="hookcheck-interrupt-") as directory:
            path = Path(directory) / "partial.json"
            client = subprocess.Popen([str(ROOT / "bin/hookcheck"), "run", "--scenario",
                str(ROOT / "examples/payment/scenario.json"), "--base-url", base,
                "--report", str(path)], env=ENV, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            # Query the fixture until the paid event is committed, then cancel
            # while the service is delaying its acknowledgement.
            deadline = time.monotonic() + 5
            while time.monotonic() < deadline:
                with LOOPBACK_HTTP.open(base + "/state", timeout=1) as response:
                    if json.load(response)["grants"] == 1:
                        break
                time.sleep(0.01)
            else:
                raise AssertionError("payment was not committed")
            client.terminate()
            stdout, stderr = client.communicate(timeout=5)
            assert client.returncode == 130, (client.returncode, stdout, stderr)
            report = json.loads(path.read_text())
            assert not report["passed"]
            assert any(a["outcome"] == "cancelled" for a in report["attempts"])
            assert any(a["outcome"] == "not_sent" for a in report["attempts"])
            print("interrupted process: exit 130; partial report retained")
    finally:
        if client is not None and client.poll() is None:
            client.kill()
            client.communicate()
        service.terminate()
        try:
            service.communicate(timeout=5)
        except subprocess.TimeoutExpired:
            service.kill()
            service.communicate()


if __name__ == "__main__":
    exercise("broken", "examples/payment/scenario.json", 1)
    exercise("fixed", "examples/payment/scenario.json", 0)
    exercise("fixed", "examples/payment/timeout.json", 1, delay="200ms")
    interrupt_run()
    if shutil.which("hurl"):
        exercise("broken", "examples/payment/scenario-hurl.json", 1, hurl=True)
        exercise("fixed", "examples/payment/scenario-hurl.json", 0, hurl=True)
    else:
        print("Hurl smoke skipped: executable not on PATH")
