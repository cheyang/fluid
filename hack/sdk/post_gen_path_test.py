#!/usr/bin/env python3
"""Contract tests for hack/sdk/post-gen.py path validation (PR #6269).

PR #6269 replaced post-gen.py's "any path inside the repo" acceptance
(os.path.commonpath) with an allow-list: --python-sdk-path must resolve
exactly to the generated SDK directory (REPO_ROOT/sdk/python), and the
script then uses that known path instead of the caller's argument.

These tests copy the production script into a sandbox repository layout
(<sandbox>/hack/sdk/post-gen.py + <sandbox>/sdk/python/fluid/models/__init__.py)
so REPO_ROOT resolves to the sandbox, then exercise the CLI exactly as
hack/sdk/gen-sdk.sh invokes it. Zero dependencies; runnable as
`python3 hack/sdk/post_gen_path_test.py` or via pytest.

Polarity: contract tests. They PASS on the PR head. On the pre-PR script
(docs/verification/fluid-6269-security-alerts/fixtures/post-gen-base.py,
from merge-base dcb76167b) the rejection tests FAIL because that script
accepted any in-repo path — that is the recorded harness-bites check.
"""
import os
import shutil
import subprocess
import sys
import tempfile

_HERE = os.path.dirname(os.path.abspath(__file__))
_REPO = os.path.dirname(os.path.dirname(_HERE))  # <repo>/hack/sdk -> <repo>
NEW_SCRIPT = os.path.join(_REPO, "hack", "sdk", "post-gen.py")
BASE_SCRIPT = os.path.join(_REPO, "docs", "verification", "fluid-6269-security-alerts",
                           "fixtures", "post-gen-base.py")

INIT_MARKER = "# import models into model package\n"
K8S_IMPORT = "# Import Kubernetes models.\nfrom kubernetes.client import *\n"


def _sandbox(script=None, with_models=True):
    script = script or NEW_SCRIPT
    tmp = tempfile.mkdtemp(prefix="postgen-test-")
    os.makedirs(os.path.join(tmp, "hack", "sdk"))
    shutil.copy(script, os.path.join(tmp, "hack", "sdk", "post-gen.py"))
    if with_models:
        os.makedirs(os.path.join(tmp, "sdk", "python", "fluid", "models"))
        with open(os.path.join(tmp, "sdk", "python", "fluid", "models", "__init__.py"), "w") as f:
            f.write(INIT_MARKER + "model_a = 1\n")
    return tmp


def _run(sandbox, arg):
    proc = subprocess.run(
        [sys.executable, os.path.join(sandbox, "hack", "sdk", "post-gen.py"),
         "--python-sdk-path=" + arg],
        cwd=sandbox, capture_output=True, text=True)
    return proc.returncode, proc.stdout, proc.stderr


def _cleanup(tmp):
    shutil.rmtree(tmp, ignore_errors=True)


def test_accepts_gen_sdk_sh_invocation():
    """Exactly what gen-sdk.sh passes: relative 'sdk/python' from repo root."""
    tmp = _sandbox()
    try:
        rc, out, err = _run(tmp, "sdk/python")
        assert rc == 0, "rc=%s stderr=%s" % (rc, err)
        assert K8S_IMPORT in open(os.path.join(tmp, "sdk", "python", "fluid", "models", "__init__.py")).read()
    finally:
        _cleanup(tmp)


def test_accepts_equivalent_spellings():
    """Trailing slash, './' prefix, absolute path and '..' normalization pass."""
    tmp = _sandbox()
    try:
        for arg in ("sdk/python/", "./sdk/python", "sdk/../sdk/python",
                    os.path.join(tmp, "sdk", "python")):
            # restore fixture each time
            with open(os.path.join(tmp, "sdk", "python", "fluid", "models", "__init__.py"), "w") as f:
                f.write(INIT_MARKER + "model_a = 1\n")
            rc, out, err = _run(tmp, arg)
            assert rc == 0, "arg=%r rc=%s stderr=%s" % (arg, rc, err)
    finally:
        _cleanup(tmp)


def test_accepts_symlink_to_generated_dir():
    """A symlink whose target IS the SDK dir resolves to it and is accepted."""
    tmp = _sandbox()
    try:
        os.symlink(os.path.join(tmp, "sdk", "python"), os.path.join(tmp, "alias"))
        rc, out, err = _run(tmp, "alias")
        assert rc == 0, "rc=%s stderr=%s" % (rc, err)
    finally:
        _cleanup(tmp)


def test_rejects_other_in_repo_paths():
    """In-repo but not the SDK dir: rejected with the allow-list message."""
    tmp = _sandbox()
    try:
        for arg in ("sdk", "hack", ".", "sdk/python/..", "no/such/dir"):
            rc, out, err = _run(tmp, arg)
            assert rc != 0, "arg=%r unexpectedly accepted" % arg
            assert "must be one of" in err, "arg=%r stderr=%r" % (arg, err)
    finally:
        _cleanup(tmp)


def test_rejects_paths_outside_repo():
    """Absolute outside dirs are rejected, not silently accepted."""
    outside = tempfile.mkdtemp(prefix="postgen-outside-")
    try:
        tmp = _sandbox()
        try:
            for arg in (outside, "/tmp", "/etc"):
                rc, out, err = _run(tmp, arg)
                assert rc != 0, "arg=%r unexpectedly accepted" % arg
                assert "must be one of" in err, "arg=%r stderr=%r" % (arg, err)
        finally:
            _cleanup(tmp)
    finally:
        _cleanup(outside)


def test_rejects_symlink_escape():
    """A symlink inside the repo pointing outside is rejected."""
    outside = tempfile.mkdtemp(prefix="postgen-evil-")
    try:
        os.makedirs(os.path.join(outside, "fluid", "models"))
        with open(os.path.join(outside, "fluid", "models", "__init__.py"), "w") as f:
            f.write(INIT_MARKER)
        tmp = _sandbox()
        try:
            os.symlink(outside, os.path.join(tmp, "sdk", "evil"))
            rc, out, err = _run(tmp, "sdk/evil")
            assert rc != 0, "symlink escape accepted"
            assert "must be one of" in err, "stderr=%r" % err
        finally:
            _cleanup(tmp)
    finally:
        _cleanup(outside)


def test_missing_argument_is_argparse_error():
    tmp = _sandbox()
    try:
        proc = subprocess.run(
            [sys.executable, os.path.join(tmp, "hack", "sdk", "post-gen.py")],
            cwd=sandbox_cwd(tmp), capture_output=True, text=True)
        assert proc.returncode != 0
    finally:
        _cleanup(tmp)


def sandbox_cwd(tmp):
    return tmp


def test_missing_models_file_fails_cleanly():
    """No fluid/models/__init__.py -> clear error, not a traceback crash."""
    tmp = _sandbox(with_models=False)
    try:
        rc, out, err = _run(tmp, "sdk/python")
        assert rc != 0
        assert "not found" in err and "generated python SDK" in err, "stderr=%r" % err
    finally:
        _cleanup(tmp)


def test_output_byte_identical_to_base_script():
    """The rewrite produces byte-identical output to the pre-PR script."""
    outputs = []
    for script in (NEW_SCRIPT, BASE_SCRIPT):
        tmp = _sandbox(script=script)
        try:
            rc, out, err = _run(tmp, "sdk/python")
            assert rc == 0, "script=%s rc=%s stderr=%s" % (script, rc, err)
            with open(os.path.join(tmp, "sdk", "python", "fluid", "models", "__init__.py"), "rb") as f:
                outputs.append(f.read())
        finally:
            _cleanup(tmp)
    assert outputs[0] == outputs[1], "new script output differs from base:\n%r\nvs\n%r" % (outputs[0], outputs[1])

def test_rejects_in_repo_decoy_dir():
    """A different in-repo dir that HAS fluid/models/__init__.py must not be
    rewritten. This is the concrete behavioral delta vs the pre-PR script,
    which accepted any in-repo path and rewrote whatever matched (its
    models-not-found error only fired when the decoy lacked the file)."""
    tmp = _sandbox()
    try:
        os.makedirs(os.path.join(tmp, "decoy", "fluid", "models"))
        decoy_init = os.path.join(tmp, "decoy", "fluid", "models", "__init__.py")
        with open(decoy_init, "w") as f:
            f.write(INIT_MARKER + "model_b = 2\n")
        rc, out, err = _run(tmp, "decoy")
        assert rc != 0, "decoy dir accepted"
        assert "must be one of" in err, "stderr=%r" % err
        assert open(decoy_init).read() == INIT_MARKER + "model_b = 2\n", "decoy file was rewritten"
    finally:
        _cleanup(tmp)


def main():
    tests = sorted(name for name in globals() if name.startswith("test_"))
    failed = []
    for name in tests:
        try:
            globals()[name]()
            print("PASS %s" % name)
        except AssertionError as e:
            failed.append((name, str(e)))
            print("FAIL %s: %s" % (name, e))
    print("\n%d/%d passed" % (len(tests) - len(failed), len(tests)))
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
