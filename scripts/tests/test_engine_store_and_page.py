"""Tests for T038i: the engine's store volume and the run page image being optional for `ap up`.

Names the implementer follows:
  compose.yaml   a top-level named volume `engine-store`, mounted (type volume, writable) at
                 /var/lib/engine on the engine and on no other service; the engine's environment has
                 ENGINE_STORE_DIR=/var/lib/engine; the engine's root stays read_only.
  Dockerfile.engine  the volume is owned by 65532 because Docker copies the image directory's owner
                 into a new named volume: the final (scratch) stage gets /var/lib/engine owned by
                 65532:65532, e.g. `COPY --from=build --chown=65532:65532 /out/store /var/lib/engine`
                 with the directory made in the build stage (scratch has no RUN).
  ap             `page` joins `optional_images`. With no page image recorded, `ap up` passes
                 `--scale page=0 --scale page-proxy=0` to `docker compose up` (neither starts), starts
                 the rest and says on stderr that the run page is not built yet (T056). With one
                 recorded it is verified, passed, and no such scale is given. `ap down` keeps the
                 volume (no -v / --volumes).
"""
import unittest

import test_compose as tc
import test_launcher as tl

ENGINE = "engine"
STORE = "/var/lib/engine"


class StoreInComposeTest(tc.ComposeTest):
    def cfg(self):
        return self.checker_input(tc.COMPOSE)

    def mounts(self, svc):
        return [v for v in svc.get("volumes") or [] if v.get("target") == STORE]

    def test_the_engine_mounts_the_named_volume_engine_store_writable(self):
        cfg = self.cfg()
        self.assertIn("engine-store", cfg.get("volumes") or {})
        (m,) = self.mounts(cfg["services"][ENGINE])
        self.assertEqual(m.get("type"), "volume")
        self.assertEqual(m.get("source"), "engine-store")
        self.assertFalse(m.get("read_only"))

    def test_no_other_service_mounts_the_store_or_names_the_volume(self):
        for name, s in self.cfg()["services"].items():
            if name == ENGINE:
                continue
            with self.subTest(service=name):
                self.assertEqual(self.mounts(s), [])
                self.assertFalse([v for v in s.get("volumes") or [] if v.get("source") == "engine-store"])

    def test_the_volume_is_a_plain_local_volume(self):
        v = (self.cfg().get("volumes") or {}).get("engine-store") or {}
        self.assertFalse(v.get("external"))
        self.assertIn(v.get("driver", "local"), ("local",))
        self.assertFalse(v.get("driver_opts"), "a bind through driver_opts is a host mount")

    def test_the_engine_gets_the_store_directory_and_keeps_a_read_only_root(self):
        s = self.cfg()["services"][ENGINE]
        self.assertEqual((s.get("environment") or {}).get("ENGINE_STORE_DIR"), STORE)
        self.assertIs(s.get("read_only"), True)
        self.assertEqual(str(s.get("user")), "65532:65532")

    def test_the_image_has_the_store_directory_owned_by_the_engine_user(self):
        """A named volume takes its owner from the image's directory; the image is `scratch`."""
        text = (tc.REPO / "src/deploy/Dockerfile.engine").read_text()
        self.assertRegex(text, r"(?m)^COPY\s+.*--chown=65532:65532\s+\S+\s+/var/lib/engine/?\s*$")
        self.assertRegex(text, r"(?m)^FROM scratch")

    def test_the_real_file_still_passes_the_hardening_checks(self):
        bad, _ = self.check(tc.COMPOSE)
        self.assertEqual(bad, {})


class PageOptionalTest(tl.RealLauncherTest):
    def scales(self, call):
        a = call["argv"]
        out = []
        for i, x in enumerate(a):
            if x == "--scale" and i + 1 < len(a):
                out.append(a[i + 1])
            elif x.startswith("--scale="):
                out.append(x[len("--scale="):])
        return out

    def test_up_without_a_page_image_starts_the_rest_and_says_so(self):
        self.write_images([n for n in tl.IMAGE_NAMES if n != "page"])
        r = self.up()
        self.assertEqual(r.returncode, 0, r.stderr)
        (c,) = self.up_calls()
        self.assertCountEqual([s for s in self.scales(c)], ["page=0", "page-proxy=0"])
        self.assertEqual(c["env"].get("AP_IMAGE_PAGE"), "ap-page:not-built")
        self.assertRegex(r.stderr, r"(?i)run page.*not built yet.*T056")
        vs = " ".join(" ".join(x["argv"]) for _, x in self.verify_calls())
        self.assertNotIn("ap-page", vs)
        self.assertIn("ap-engine", vs)

    def test_up_with_a_page_image_verifies_passes_and_starts_it(self):
        r = self.up()
        self.assertEqual(r.returncode, 0, r.stderr)
        (c,) = self.up_calls()
        self.assertEqual(self.scales(c), [])
        self.assertEqual(c["env"]["AP_IMAGE_PAGE"], self.recorded()["AP_IMAGE_PAGE"])
        vs = " ".join(" ".join(x["argv"]) for _, x in self.verify_calls())
        self.assertIn("ap-page@sha256", vs)
        self.assertNotRegex(r.stderr, r"(?i)not built yet")

    def test_a_recorded_page_image_that_fails_verification_still_stops_up(self):
        r = self.up(STUB_VERIFY_FAIL="/ap-page@")
        self.assertNotEqual(r.returncode, 0)
        self.assertEqual(self.up_calls(), [])

    def test_engine_and_cli_are_still_required_without_a_page(self):
        for n in ("engine", "cli"):
            with self.subTest(missing=n):
                self.setUp()
                self.write_images([m for m in tl.IMAGE_NAMES if m not in ("page", n)])
                r = self.up()
                self.assertEqual(r.returncode, 1, r.stderr)
                self.assertIn("build.sh %s" % n, r.stderr)
                self.assertEqual(self.up_calls(), [])

    def test_down_keeps_the_volumes(self):
        self.assertEqual(self.up().returncode, 0)
        self.clear()
        self.assertEqual(self.ap("down").returncode, 0)
        downs = [c for c in self.calls() if c["argv"][:1] == ["compose"] and "down" in c["argv"]]
        self.assertTrue(downs)
        for c in downs:
            self.assertFalse({"-v", "--volumes"} & set(c["argv"]), c["argv"])


for _cls in (StoreInComposeTest, PageOptionalTest):
    _base = tc.ComposeTest if _cls is StoreInComposeTest else tl.RealLauncherTest
    for _n in [n for n in dir(_base) if n.startswith("test_")]:
        setattr(_cls, _n, None)

if __name__ == "__main__":
    unittest.main()
