"""Regression: the offline install of go-arch-lint must not need the checksum database (T098, D40)."""
import os
import shutil
import unittest

from test_arch_lint import ArchTestCase, FILE_PROXY, rmtree


class ArchOfflineSumdbTest(ArchTestCase):
    def test_offline_install_without_cached_sumdb(self):
        xdg, dst = self.seeded_cache()
        # Remove the copy's directories only; the hard-linked files stay in the user's cache.
        sumdb = os.path.join(dst, "pkg", "mod", "cache", "download", "sumdb")
        self.assertTrue(os.path.realpath(sumdb).startswith(os.path.realpath(xdg)), sumdb)
        rmtree(sumdb)
        self.assertFalse(os.path.exists(sumdb))
        shutil.rmtree(os.path.join(dst, "bin", "go-arch-lint"), ignore_errors=True)
        self.assertFalse(os.path.exists(os.path.join(dst, "bin", "go-arch-lint")))
        r = self.run_step(xdg=xdg)
        self.assertEqual(r.returncode, 0, r.stdout)
        installs = [" ".join(c) for c in r.docker_calls
                    if "go-arch-lint" in " ".join(c) and "install" in " ".join(c)]
        self.assertTrue(installs, "no docker call installs go-arch-lint")
        for c in installs:
            self.assertRegex(c, r"--network\s+none")
            self.assertIn(FILE_PROXY, c)
        self.assertTrue([c for c in r.docker_calls if "go-arch-lint" in " ".join(c)
                         and "check" in " ".join(c) and "install" not in " ".join(c)],
                        "no docker call runs go-arch-lint check")


if __name__ == "__main__":
    unittest.main()
