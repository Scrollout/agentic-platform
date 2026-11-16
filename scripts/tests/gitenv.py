"""Environment for test subprocesses that touch throwaway git repositories.

Git exports GIT_DIR, GIT_INDEX_FILE and friends to hooks. Anything inherited would point a
throwaway repository's git commands at the enclosing repository, so every GIT_* variable is
dropped and HOME is a scratch directory, keeping global configuration out of reach.
"""
import os
import tempfile


def clean_env(home):
    env = {k: v for k, v in os.environ.items() if not k.startswith("GIT_")}
    env["HOME"] = home
    env["XDG_CONFIG_HOME"] = os.path.join(home, ".config")
    env["GIT_CONFIG_NOSYSTEM"] = "1"
    env["GIT_CONFIG_GLOBAL"] = os.devnull
    return env


def make_home(testcase):
    tmp = tempfile.TemporaryDirectory()
    testcase.addCleanup(tmp.cleanup)
    return tmp.name


def stub_dir(testcase, stubs):
    """Make a directory of executable stub commands, {name: sh body}, and return its path.

    Put it first on PATH with with_path(); the stub can reach the real command by the absolute
    path the test resolved before the stub existed.
    """
    tmp = tempfile.TemporaryDirectory()
    testcase.addCleanup(tmp.cleanup)
    for name, body in stubs.items():
        path = os.path.join(tmp.name, name)
        with open(path, "w") as f:
            f.write("#!/bin/sh\n" + body)
        os.chmod(path, 0o755)
    return tmp.name


def with_path(env, directory):
    """A copy of env whose PATH starts with directory."""
    out = dict(env)
    out["PATH"] = directory + os.pathsep + env.get("PATH", os.defpath)
    return out
