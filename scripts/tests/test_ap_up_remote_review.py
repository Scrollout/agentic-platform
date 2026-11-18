"""Regression tests from the fresh review of feature 003, User Story 4.

Contract src/cli/contract.md "up: remote access": on a started session any change of remote access
(on or off, another address, names or certificate) is refused, telling to run `down` first;
data-model.md "State transitions": remote --ap up (same)--> remote (nothing changes).
Reuses the harness of test_ap_up_remote.py.
"""
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from test_ap_start import PASSWORD  # noqa: E402
from test_ap_up_remote import RemoteCase, PEM_CERT, PEM_KEY  # noqa: E402

PEM_KEY2 = "-----BEGIN PRIVATE KEY-----\nb3RoZXIta2V5\n-----END PRIVATE KEY-----\n"


class CertificateChangeOnStartedSessionTest(RemoteCase):
    def developer_start(self):
        self.c, self.k = self.base / "dev-cert.pem", self.base / "dev-key.pem"
        self.c.write_text(PEM_CERT)
        self.k.write_text(PEM_KEY)
        r = self.up_remote("--certificate", str(self.c), "--key", str(self.k))
        self.assertEqual(r.returncode, 0, r.stderr)
        self.before = (self.session.read_text(), self.access_lines())

    def assertNothingChanged(self, r, mark, words):
        self.assertEqual(r.returncode, 3, r.stderr)
        self.assertNotIn("usage:", r.stderr)
        for w in words:
            self.assertIn(w, r.stderr)
        made = self.calls()[mark:]
        self.assertEqual([c for c in made if c["argv"][:1] == ["compose"] and "up" in c["argv"]], [],
                         "compose up was called")
        self.assertEqual([c for c in made if self.is_cert_make(c)], [], "a certificate was made")
        self.assertEqual([c for c in made if c["argv"][:2] == ["buildx", "build"]], [], "an image was built")
        self.assertEqual((self.page_dir / "cert.pem").read_text(), PEM_CERT, "cert.pem changed")
        self.assertEqual((self.page_dir / "key.pem").read_text(), PEM_KEY, "key.pem changed")
        self.assertEqual(self.session.read_text(), self.before[0], "the session changed")
        self.assertEqual(self.access_lines(), self.before[1], "access.log changed")

    def test_second_up_without_certificate_is_refused_and_keeps_the_developers(self):
        self.developer_start()
        mark = self.mark()
        r = self.launch("up", "--listen", "0.0.0.0", COSIGN_PASSWORD=PASSWORD)
        self.assertNothingChanged(r, mark, ["down"])
        self.assertRegex(r.stderr, r"--certificate|certificate")

    def test_second_up_with_another_key_is_refused(self):
        self.developer_start()
        k2 = self.base / "dev-key2.pem"
        k2.write_text(PEM_KEY2)
        mark = self.mark()
        r = self.launch("up", "--listen", "0.0.0.0", "--certificate", str(self.c), "--key", str(k2),
                        COSIGN_PASSWORD=PASSWORD)
        self.assertNothingChanged(r, mark, ["down"])

    def test_start_without_certificate_is_refused_before_any_build(self):
        self.developer_start()
        mark = self.mark()
        r = self.launch("start", "--listen", "0.0.0.0", COSIGN_PASSWORD=PASSWORD)
        self.assertNothingChanged(r, mark, ["down"])
        self.assertRegex(r.stderr, r"--certificate|certificate")

    def test_control_the_same_certificate_and_key_again_is_accepted(self):
        self.developer_start()
        r = self.launch("up", "--listen", "0.0.0.0", "--certificate", str(self.c), "--key", str(self.k),
                        COSIGN_PASSWORD=PASSWORD)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual((self.page_dir / "cert.pem").read_text(), PEM_CERT)
        self.assertEqual((self.page_dir / "key.pem").read_text(), PEM_KEY)


if __name__ == "__main__":
    unittest.main()
