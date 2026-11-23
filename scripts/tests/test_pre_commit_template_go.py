"""The pre-commit hook and the Go files of a pattern's module template.

A pattern's module template, src/catalogue/<pattern>/template/, holds Go files that are copied
into generated projects. It has no go.mod, so it is in no module of src/go.work and go vet cannot
judge it (it compiles only inside a generated project). The hook therefore does not refuse a
staged .go file there as "in no module": it runs gofmt -l on it and refuses the commit, naming
every such file that is not gofmt-formatted. Every other staged Go file in no module is still
refused as "in no module". gofmt skips testdata/ folders, as it does for the modules.

Each test uses a throwaway clone with the hook under test (test_pre_commit.CloneCase). Run from
the repository root with
`python3 -m unittest discover -s scripts/tests -p 'test_pre_commit_template_go.py'`.
Docker and the pinned Go image are prerequisites, as for the other hook tests.
"""
import unittest

import test_pre_commit as base

GO_OK = "package gen\n\nfunc F() int {\n\treturn 1\n}\n"
GO_UNFORMATTED = "package gen\n\nfunc   F( ) int {\n\treturn   1\n}\n"
GO_VET_VIOLATION = 'package zzplanted\n\nimport "fmt"\n\nfunc F() {\n\tfmt.Printf("%d\\n", "s")\n}\n'
NO_MODULE = "in no module"
NOT_FORMATTED = "not formatted"
T = "src/catalogue/api-feature-go/template/"


class TemplateGoCase(base.CloneCase):
    def refused(self, r, *named):
        out = self.out(r)
        self.assertNotEqual(r.returncode, 0, out)
        for n in named:
            self.assertIn(n, out)
        return out


class TemplateFormattingTest(TemplateGoCase):
    # 1
    def test_a_formatted_go_file_in_a_template_passes_the_go_gate(self):
        self.stage(T + "gen/x.go", GO_OK)
        r = self.commit()
        out = self.out(r)
        self.assertEqual(r.returncode, 0, out)
        self.assertNotIn(NO_MODULE, out)
        self.assertIn(T + "gen/x.go", self.committed_files())

    # 2
    def test_an_unformatted_go_file_in_a_template_is_refused_and_not_committed(self):
        self.stage(T + "gen/x.go", GO_UNFORMATTED)
        before = self.head()
        out = self.refused(self.commit(), "gen/x.go")
        self.assertNotIn(NO_MODULE, out)
        self.assertEqual(self.head(), before)

    # 2, every file is named
    def test_every_unformatted_template_file_is_named_and_a_formatted_one_is_not(self):
        self.stage(T + "gen/a.go", GO_UNFORMATTED)
        self.stage(T + "gen/b.go", GO_OK)
        self.stage(T + "deep/er/c.go", GO_UNFORMATTED)
        out = self.refused(self.run_hook(), "gen/a.go", "deep/er/c.go")
        self.assertNotIn("gen/b.go", out)

    # edge: a name with a space
    def test_an_unformatted_template_file_with_a_space_in_its_name_is_named(self):
        self.stage(T + "gen/a b.go", GO_UNFORMATTED)
        out = self.refused(self.run_hook(), "gen/a b.go")
        self.assertNotIn(NO_MODULE, out)


class OutsideATemplateTest(TemplateGoCase):
    # 3
    def test_a_formatted_go_file_in_no_module_outside_a_template_is_still_refused(self):
        for path in ("src/catalogue/api-feature-go/x.go", "src/other/x.go",
                     "src/catalogue/api-feature-go/template.go",
                     "src/other/template/x.go", "src/catalogue/template/x.go"):
            with self.subTest(path=path):
                self.git("reset", "-q")
                self.stage(path, GO_OK)
                self.refused(self.run_hook(), NO_MODULE, path)


class TemplateWithModuleTest(TemplateGoCase):
    # 4
    def test_a_vet_violation_in_a_module_is_refused_beside_a_formatted_template_file(self):
        self.stage(T + "gen/x.go", GO_OK)
        self.stage("src/slice/zzplanted/vetbad.go", GO_VET_VIOLATION)
        out = self.refused(self.run_hook(), "Printf")
        self.assertNotIn(NO_MODULE, out)

    def test_a_gofmt_violation_in_a_module_is_refused_beside_a_formatted_template_file(self):
        self.stage(T + "gen/x.go", GO_OK)
        self.stage("src/slice/zzplanted/bad.go", GO_UNFORMATTED)
        out = self.refused(self.run_hook(), "zzplanted/bad.go")
        self.assertNotIn(NO_MODULE, out)

    def test_an_unformatted_template_file_is_refused_beside_a_clean_module_file(self):
        self.stage(T + "gen/x.go", GO_UNFORMATTED)
        self.stage("src/slice/zzplanted/ok.go", GO_OK.replace("gen", "zzplanted"))
        out = self.refused(self.run_hook(), "gen/x.go")
        self.assertNotIn(NO_MODULE, out)

    def test_formatted_template_and_clean_module_files_pass_together(self):
        self.stage(T + "gen/x.go", GO_OK)
        self.stage("src/slice/zzplanted/ok.go", GO_OK.replace("gen", "zzplanted"))
        r = self.run_hook()
        self.assertEqual(r.returncode, 0, self.out(r))

    def test_a_go_file_in_no_module_is_refused_beside_a_formatted_template_file(self):
        self.stage(T + "gen/x.go", GO_OK)
        self.stage("src/other/x.go", GO_OK)
        self.refused(self.run_hook(), NO_MODULE, "src/other/x.go")


class NotAffectedTest(TemplateGoCase):
    # 5
    def test_an_unformatted_go_file_under_a_templates_testdata_is_not_judged(self):
        self.stage(T + "testdata/x.go", GO_UNFORMATTED)
        r = self.run_hook()
        out = self.out(r)
        self.assertEqual(r.returncode, 0, out)
        self.assertNotIn(NO_MODULE, out)

    def test_a_non_go_file_under_a_template_is_not_judged_as_go(self):
        self.stage(T + "notes.txt", "package   gen   not go\n")
        r = self.run_hook()
        self.assertEqual(r.returncode, 0, self.out(r))

    # edge: a deletion is not judged by gofmt
    def test_deleting_a_template_go_file_passes(self):
        self.raw_commit(T + "gen/y.go", GO_OK)
        self.git("rm", "-q", T + "gen/y.go")
        r = self.run_hook()
        out = self.out(r)
        self.assertEqual(r.returncode, 0, out)
        self.assertNotIn(NO_MODULE, out)


if __name__ == "__main__":
    unittest.main()
