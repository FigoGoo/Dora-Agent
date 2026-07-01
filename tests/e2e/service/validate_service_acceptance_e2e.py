#!/usr/bin/env python3
from __future__ import annotations

import os
import subprocess
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[3]


def main() -> int:
    env = os.environ.copy()
    goroot = env.get("GOROOT", "/Users/figo/sdk/go1.26.3")
    gopath = env.get("GOPATH", "/Users/figo/go")
    env["GOROOT"] = goroot
    env["GOPATH"] = gopath
    env["PATH"] = f"{goroot}/bin:{gopath}/bin:{env.get('PATH', '')}"

    cmd = [
        "go",
        "test",
        "-count=1",
        "./services/agent/internal/application/workbench",
        "-run",
        "Test(IndependentToolCharge|SkillTestConsumesReviewCandidateRPC)",
    ]
    completed = subprocess.run(cmd, cwd=ROOT, env=env, text=True)
    if completed.returncode != 0:
        return completed.returncode
    print("service acceptance service e2e checks ok: Agent -> BusinessGateway mock covers model list, review skill spec, tool estimate/freeze/charge/release")
    return 0


if __name__ == "__main__":
    sys.exit(main())
