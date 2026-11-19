# Verification harness for PR #6269 (reviewer: codex). ADDITIVE test file; production
# code is untouched. Exercises hack/sdk/post-gen.py's resolve_sdk_path() allow-list
# and the end-to-end rewrite behavior of the script.
#
# Polarity: CONTRACT tests — they assert the intended (post-PR) behavior.
#   - On the PR head they must PASS.
#   - Run against the BASE script (hack/sdk/post-gen.base.py, same dir depth so its REPO_ROOT matches)
#     the accept/reject matrix FAILS for the rejected-but-in-repo cases (base allowed any
#     in-repo path), proving the tests actually bite.
import importlib.util
import os
import shutil
import subprocess
import sys
import sysconfig
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
REPO_ROOT = os.path.realpath(os.path.join(HERE, "..", "..", ".."))
SCRIPT = os.environ.get("POST_GEN_SCRIPT", os.path.join(REPO_ROOT, "hack", "sdk", "post-gen.py"))


def load_module(script):
    spec = importlib.util.spec_from_file_location("post_gen_under_test", script)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


class ResolveSdkPathTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.mod = load_module(SCRIPT)
        cls.sdk_dir = os.path.join(cls.mod.REPO_ROOT, "sdk", "python")

    def resolve(self, arg, cwd=None):
        # resolve_sdk_path resolves relative paths against the process CWD, like the CLI does.
        old = os.getcwd()
        try:
            if cwd:
                os.chdir(cwd)
            return self.mod.resolve_sdk_path(arg)
        finally:
            os.chdir(old)

    def test_accepts_the_generated_sdk_path_spellings(self):
        with tempfile.TemporaryDirectory() as tmp:
            # realpath() resolves relative paths against CWD; run from repo root like gen-sdk.sh does.
            for spelling in ("sdk/python", "./sdk/python", "sdk/python/", "sdk/../sdk/python",
                             os.path.join("sdk", ".", "python")):
                self.assertEqual(self.sdk_dir, self.resolve(spelling, cwd=REPO_ROOT), spelling)
            # absolute path
            self.assertEqual(self.sdk_dir, self.resolve(self.sdk_dir))
            # absolute path with dot segments
            self.assertEqual(self.sdk_dir, self.resolve(os.path.join(REPO_ROOT, "sdk", "..", "sdk", "python")))
            # symlinked repo checkout path still resolves to the SDK dir
            link = os.path.join(tmp, "repo-link")
            os.symlink(REPO_ROOT, link)
            self.assertEqual(self.sdk_dir, self.resolve(os.path.join(link, "sdk", "python")))

    def test_rejects_everything_else(self):
        rejected = [
            "sdk", "hack", ".", "..", "/", "sdk/java",
            os.path.join("sdk", "python2"),  # prefix-sibling must not pass
            tempfile.gettempdir(),
        ]
        for spelling in rejected:
            with self.assertRaises(SystemExit, msg=spelling):
                self.resolve(spelling, cwd=REPO_ROOT)
        # absolute outside path
        with self.assertRaises(SystemExit):
            self.resolve(tempfile.gettempdir())
        # symlink escape: a symlink named like the SDK but pointing outside the repo
        with tempfile.TemporaryDirectory() as tmp:
            outside = os.path.join(tmp, "outside")
            os.makedirs(outside)
            evil = os.path.join(REPO_ROOT, "sdk", "python-evil-symlink")
            os.symlink(outside, evil)
            try:
                with self.assertRaises(SystemExit):
                    self.resolve(os.path.join("sdk", "python-evil-symlink"), cwd=REPO_ROOT)
            finally:
                os.unlink(evil)

    def test_returned_path_is_the_constant_not_user_input(self):
        # The sanitizer property SonarCloud needs: the returned value must be the allow-listed
        # constant, so no user-controlled string flows into open().
        got = self.resolve(os.path.join(REPO_ROOT, "sdk", ".", "python"))
        self.assertIn(got, self.mod.PYTHON_SDK_DIRS)


FIXTURE_MODELS_INIT = """# flake8: noqa

# import models into model package
from fluid.models.alluxio_runtime import AlluxioRuntime
"""


class CliBehaviorTest(unittest.TestCase):
    """Run the script as a CLI the way gen-sdk.sh does."""

    def setUp(self):
        self.sdk_dir = os.path.join(REPO_ROOT, "sdk", "python")
        self.models_dir = os.path.join(self.sdk_dir, "fluid", "models")
        os.makedirs(self.models_dir, exist_ok=True)
        self.init_py = os.path.join(self.models_dir, "__init__.py")
        with open(self.init_py, "w") as f:
            f.write(FIXTURE_MODELS_INIT)

    def tearDown(self):
        shutil.rmtree(os.path.join(self.sdk_dir, "fluid"), ignore_errors=True)

    def run_cli(self, *args, cwd=REPO_ROOT):
        return subprocess.run([sys.executable, SCRIPT] + list(args),
                              cwd=cwd, capture_output=True, text=True)

    def test_missing_argument_is_argparse_error(self):
        proc = self.run_cli()
        self.assertEqual(2, proc.returncode)
        self.assertIn("required", proc.stderr)

    def test_rewrite_inserts_kubernetes_import(self):
        proc = self.run_cli("--python-sdk-path=sdk/python")
        self.assertEqual(0, proc.returncode, proc.stderr)
        with open(self.init_py) as f:
            content = f.read()
        self.assertIn("# Import Kubernetes models.\nfrom kubernetes.client import *\n", content)
        # marker line kept, model imports kept
        self.assertIn("# import models into model package\n", content)
        self.assertIn("from fluid.models.alluxio_runtime import AlluxioRuntime\n", content)

    def test_rejected_path_exits_nonzero_without_writing(self):
        with tempfile.TemporaryDirectory() as tmp:
            proc = self.run_cli("--python-sdk-path=%s" % tmp)
            self.assertNotEqual(0, proc.returncode)
            self.assertIn("must be one of", proc.stderr + proc.stdout)
            self.assertEqual([], os.listdir(tmp))

    def test_head_output_byte_identical_to_base_script(self):
        base_script = os.path.join(REPO_ROOT, "docs", "verification", "pr6269-security-alerts",
                                   "..", "..", "..", "hack", "sdk", "post-gen.base.py")
        if not os.path.isfile(base_script):
            self.skipTest("base script snapshot not present")
        outs = []
        for script in (base_script, SCRIPT):
            with open(self.init_py, "w") as f:
                f.write(FIXTURE_MODELS_INIT)
            proc = subprocess.run([sys.executable, script, "--python-sdk-path=sdk/python"],
                                  cwd=REPO_ROOT, capture_output=True, text=True)
            self.assertEqual(0, proc.returncode, proc.stderr)
            with open(self.init_py) as f:
                outs.append((proc.stdout, f.read()))
        self.assertEqual(outs[0], outs[1], "head script output differs from base script output")


if __name__ == "__main__":
    unittest.main()
