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
