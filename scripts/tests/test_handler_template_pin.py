"""Test for the label of `src/catalogue/api-feature-go/template/gen/std-http-handler.tmpl`
(feature 010, task T006).

The template is oapi-codegen's `stdhttp/std-http-handler.tmpl` with one change. Its first line is a
Go-template comment naming the release it was copied from, for example
`{{/* copied from oapi-codegen v2.8.2 pkg/codegen/templates/stdhttp/std-http-handler.tmpl; one
change: refuseMalformed */}}`, and the release must be the one `src/deploy/images.lock` pins (the
`oapi-codegen` line, `...@v2.8.2`).

This test proves the label only: that the release named in the first line is the pinned one. It
does not prove that the rest of the file is that release's text plus the one change; the reviewer
of each oapi-codegen bump checks that.

Cases:
- label_release / pinned_release (small functions) on fixtures: clean and planted.
- the comparison names both releases when they differ, and the file and pin it compared.
- the real files (RealFilesTest) report the file and the pin they compared.
Edge cases checked: a pin with a digest after the version, a pin line among comments and other
tools, v2.8.20 against v2.8.2 (whole-token compare, not a prefix), a missing or non-comment first
line, an empty file, a comment with extra whitespace, CRLF line ending, a label naming two
releases, a lock without an oapi-codegen line, a lock with two such lines.
"""
import os
import re
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(os.path.dirname(HERE))
TEMPLATE = os.path.join(ROOT, "src/catalogue/api-feature-go/template/gen/std-http-handler.tmpl")
LOCK = os.path.join(ROOT, "src/deploy/images.lock")

RELEASE = r"v[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?"


def pinned_release(lock_text):
    """The release the `oapi-codegen` line of images.lock pins, or raise ValueError."""
    found = []
    for line in lock_text.splitlines():
        words = line.split()
        if len(words) >= 2 and words[0] == "oapi-codegen":
            m = re.fullmatch(r".*/oapi-codegen/v2/cmd/oapi-codegen@(" + RELEASE + r")(?:@\S+)?", words[1])
            if not m:
                raise ValueError("the oapi-codegen line names no release: " + line)
            found.append(m.group(1))
    if len(found) != 1:
        raise ValueError("images.lock must hold exactly one oapi-codegen line, found %d" % len(found))
    return found[0]


def label_release(template_text):
    """The release the first line's template comment says the file was copied from, or raise ValueError."""
    first = template_text.split("\n", 1)[0].rstrip("\r")
    m = re.fullmatch(r"\{\{-?\s*/\*(.*)\*/\s*-?\}\}", first.strip())
    if not m:
        raise ValueError("the first line is not a template comment: " + repr(first))
    named = re.findall(r"(?<![0-9A-Za-z.])oapi-codegen\s+(" + RELEASE + r")(?![0-9A-Za-z.])", m.group(1))
    if len(named) != 1:
        raise ValueError("the first line must name one oapi-codegen release, found %d: %r" % (len(named), first))
    return named[0]


def compare(template_text, lock_text, template_name=TEMPLATE, lock_name=LOCK):
    """Return None when the label names the pinned release, else a message naming both."""
    pin = pinned_release(lock_text)
    label = label_release(template_text)
    if label != pin:
        return "%s names oapi-codegen %s but %s pins %s" % (template_name, label, lock_name, pin)
    return None


LOCK_OK = (
    "# Go tools\n"
    "godog                      github.com/cucumber/godog/cmd/godog@v0.16.0\n"
    "oapi-codegen               github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.2\n"
)
CLEAN = "{{/* copied from oapi-codegen v2.8.2 pkg/codegen/templates/stdhttp/std-http-handler.tmpl; one change: refuseMalformed */}}\nbody\n"


class CompareTest(unittest.TestCase):
    def test_clean_fixture_passes(self):
        self.assertIsNone(compare(CLEAN, LOCK_OK))

    def test_another_release_fails_naming_both(self):
        msg = compare(CLEAN.replace("v2.8.2", "v2.7.0"), LOCK_OK, "T.tmpl", "L.lock")
        self.assertIsNotNone(msg)
        for s in ("T.tmpl", "L.lock", "v2.7.0", "v2.8.2"):
            self.assertIn(s, msg)

    def test_prefix_is_not_a_match(self):
        self.assertIsNotNone(compare(CLEAN.replace("v2.8.2", "v2.8.20"), LOCK_OK))
        self.assertIsNotNone(compare(CLEAN, LOCK_OK.replace("v2.8.2", "v2.8.20")))

    def test_pin_with_a_digest(self):
        lock = LOCK_OK.replace("@v2.8.2", "@v2.8.2@sha256:" + "a" * 64)
        self.assertIsNone(compare(CLEAN, lock))
        self.assertEqual(pinned_release(lock), "v2.8.2")

    def test_pin_among_comments_and_other_tools(self):
        lock = "# oapi-codegen v9.9.9 in a comment\n" + LOCK_OK + "govulncheck x/vuln@v1.9.0\n"
        self.assertEqual(pinned_release(lock), "v2.8.2")

    def test_lock_without_or_with_two_pins_is_an_error(self):
        with self.assertRaises(ValueError):
            pinned_release("godog github.com/cucumber/godog/cmd/godog@v0.16.0\n")
        with self.assertRaises(ValueError):
            pinned_release(LOCK_OK + LOCK_OK)
        with self.assertRaises(ValueError):
            pinned_release("oapi-codegen   github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest\n")

    def test_label_whitespace_and_trim_markers(self):
        for first in (
            "{{/*copied from oapi-codegen v2.8.2 x*/}}",
            "  {{/*   copied from   oapi-codegen   v2.8.2   x   */}}  ",
            "{{- /* copied from oapi-codegen v2.8.2 */ -}}",
            "{{/* copied from oapi-codegen v2.8.2 */}}\r",
        ):
            self.assertEqual(label_release(first + "\nrest\n"), "v2.8.2", first)

    def test_missing_or_wrong_first_line_is_an_error(self):
        for text in (
            "",
            "\n{{/* copied from oapi-codegen v2.8.2 */}}\n",  # comment on the second line
            "package gen\n{{/* copied from oapi-codegen v2.8.2 */}}\n",
            "// copied from oapi-codegen v2.8.2\n",  # not a template comment
            "{{/* copied from somewhere */}}\n",  # no release
            "{{/* copied from oapi-codegen v2.8.2 and oapi-codegen v2.7.0 */}}\n",  # two releases
            "{{/* copied from oapi-codegen v2.8 */}}\n",
            "{{ .Name }} oapi-codegen v2.8.2\n",
        ):
            with self.assertRaises(ValueError, msg=repr(text)):
                label_release(text)


class RealFilesTest(unittest.TestCase):
    def test_template_names_the_pinned_release(self):
        """Reports the file and the pin it compared; fails when the template is missing or differs."""
        self.assertTrue(os.path.isfile(TEMPLATE), "missing: " + TEMPLATE)
        with open(TEMPLATE, encoding="utf-8") as f:
            template = f.read()
        with open(LOCK, encoding="utf-8") as f:
            lock = f.read()
        pin = pinned_release(lock)
        print("compared %s with the pin %s of %s" % (TEMPLATE, pin, LOCK))
        msg = compare(template, lock)
        self.assertIsNone(msg, msg)


if __name__ == "__main__":
    unittest.main()
