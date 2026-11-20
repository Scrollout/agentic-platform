// T035: the lab's settings for the durable services (DurableSettings) are a second copy of
// compose.yaml's; this file proves them equal, as platform_settings_test.go does for
// ServiceSettings. It has no build tag and runs without Docker.
//
// Compared, for durable-store, durable-schema and durable: read_only, user, cap_drop,
// security_opt, tmpfs, cpus, memory, pids, environment (both ways), entrypoint, command, and the
// health check: HealthCmd is the shell command of compose.yaml's `healthcheck.test` (`CMD-SHELL`
// string, or the `CMD` list joined by spaces; no health check is ""), and Healthy is its
// `start_period` (compared as durations). x-hardened is read through the YAML merge key.
//
// NOT compared, because DurableSettings says they are the lab's own (platform.go): the password
// file and its secret, the mounts (volumes), the networks and aliases, the images and their pins,
// depends_on, restart, and the health check's interval, timeout and retries (the lab sets one
// value for all three services; Settings does not model them).
package lab

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

// yamlStrings reads a Compose list of strings, or a single string (split on spaces, as Compose
// splits a string command), or nothing.
type yamlStrings []string

func (s *yamlStrings) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		*s = strings.Fields(n.Value)
		return nil
	}
	var l []string
	if err := n.Decode(&l); err != nil {
		return err
	}
	*s = l
	return nil
}

// yamlEnv reads Compose's environment as a map or as a list of NAME=value.
type yamlEnv map[string]string

func (e *yamlEnv) UnmarshalYAML(n *yaml.Node) error {
	out := map[string]string{}
	if n.Kind == yaml.SequenceNode {
		var l []string
		if err := n.Decode(&l); err != nil {
			return err
		}
		for _, kv := range l {
			k, v, _ := strings.Cut(kv, "=")
			out[k] = v
		}
	} else if err := n.Decode(&out); err != nil {
		return err
	}
	*e = out
	return nil
}

type durableCompose struct {
	ReadOnly    bool        `yaml:"read_only"`
	User        string      `yaml:"user"`
	CapDrop     []string    `yaml:"cap_drop"`
	SecurityOpt []string    `yaml:"security_opt"`
	Tmpfs       yamlStrings `yaml:"tmpfs"`
	Entrypoint  yamlStrings `yaml:"entrypoint"`
	Command     yamlStrings `yaml:"command"`
	Environment yamlEnv     `yaml:"environment"`
	CPUs        string      `yaml:"cpus"`
	MemLimit    string      `yaml:"mem_limit"`
	PidsLimit   int         `yaml:"pids_limit"`
	Deploy      struct {
		Resources struct {
			Limits struct {
				CPUs   string `yaml:"cpus"`
				Memory string `yaml:"memory"`
				Pids   int    `yaml:"pids"`
			} `yaml:"limits"`
		} `yaml:"resources"`
	} `yaml:"deploy"`
	Healthcheck *struct {
		Test        yamlStrings `yaml:"test"`
		StartPeriod string      `yaml:"start_period"`
	} `yaml:"healthcheck"`
}

func readDurableCompose(t testing.TB) map[string]durableCompose {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "src", "deploy", "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Services map[string]durableCompose `yaml:"services"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatalf("compose.yaml: %v", err)
	}
	return doc.Services
}

// recorder collects what interpolate reports instead of failing the test.
type recorder struct {
	testing.TB
	errs []string
}

func (r *recorder) Errorf(f string, a ...any) { r.errs = append(r.errs, fmt.Sprintf(f, a...)) }

// healthCmd is the shell command of a Compose health check test, "" for none.
func healthCmd(test []string) string {
	if len(test) == 0 || test[0] == "NONE" {
		return ""
	}
	switch test[0] {
	case "CMD-SHELL":
		return strings.Join(test[1:], " ")
	case "CMD":
		return strings.Join(test[1:], " ")
	}
	return strings.Join(test, " ") // string form
}

// wantDurable is what compose.yaml declares for the service, as Settings.
func wantDurable(t testing.TB, svc durableCompose) Settings {
	t.Helper()
	ip := func(l []string) []string {
		var out []string
		for _, s := range l {
			out = append(out, interpolate(t, s, map[string]string{}))
		}
		return out
	}
	w := Settings{
		ReadOnly:    svc.ReadOnly,
		User:        interpolate(t, svc.User, map[string]string{}),
		CapDrop:     svc.CapDrop,
		SecurityOpt: svc.SecurityOpt,
		Tmpfs:       svc.Tmpfs,
		CPUs:        svc.Deploy.Resources.Limits.CPUs,
		Memory:      svc.Deploy.Resources.Limits.Memory,
		Pids:        svc.Deploy.Resources.Limits.Pids,
		Entrypoint:  ip(svc.Entrypoint),
		Command:     ip(svc.Command),
		Env:         map[string]string{},
	}
	if w.CPUs == "" {
		w.CPUs = svc.CPUs
	}
	if w.Memory == "" {
		w.Memory = svc.MemLimit
	}
	if w.Pids == 0 {
		w.Pids = svc.PidsLimit
	}
	for k, v := range svc.Environment {
		w.Env[k] = interpolate(t, v, map[string]string{})
	}
	if svc.Healthcheck != nil {
		w.HealthCmd = interpolate(t, healthCmd(svc.Healthcheck.Test), map[string]string{})
		w.Healthy = svc.Healthcheck.StartPeriod
	}
	return w
}

func sameDuration(a, b string) bool {
	if a == b {
		return true
	}
	da, ea := time.ParseDuration(a)
	db, eb := time.ParseDuration(b)
	return ea == nil && eb == nil && da == db
}

// differences lists every way got differs from want; empty means equal.
func differences(service string, got, want Settings) []string {
	var d []string
	check := func(field string, g, w any) {
		if !reflect.DeepEqual(g, w) {
			d = append(d, fmt.Sprintf("%s: %s = %#v, compose.yaml says %#v", service, field, g, w))
		}
	}
	check("read-only root", got.ReadOnly, want.ReadOnly)
	check("user", got.User, want.User)
	check("dropped capabilities", sorted(got.CapDrop), sorted(want.CapDrop))
	check("security options", normalOpts(got.SecurityOpt), normalOpts(want.SecurityOpt))
	check("tmpfs", sorted(got.Tmpfs), sorted(want.Tmpfs))
	check("CPUs", got.CPUs, want.CPUs)
	check("memory", got.Memory, want.Memory)
	check("process limit", got.Pids, want.Pids)
	check("entrypoint", got.Entrypoint, want.Entrypoint)
	if len(got.Command) != 0 || len(want.Command) != 0 {
		check("command", got.Command, want.Command)
	}
	check("health command", got.HealthCmd, want.HealthCmd)
	if got.HealthCmd != "" || want.HealthCmd != "" {
		if !sameDuration(got.Healthy, want.Healthy) {
			d = append(d, fmt.Sprintf("%s: health start period = %q, compose.yaml says %q", service, got.Healthy, want.Healthy))
		}
	}
	for k, w := range want.Env {
		if g, ok := got.Env[k]; !ok {
			d = append(d, fmt.Sprintf("%s: environment has no %s; compose.yaml sets it to %q", service, k, w))
		} else if g != w {
			d = append(d, fmt.Sprintf("%s: environment %s = %q, compose.yaml says %q", service, k, g, w))
		}
	}
	for k, g := range got.Env {
		if _, ok := want.Env[k]; !ok {
			d = append(d, fmt.Sprintf("%s: environment has %s=%q, which compose.yaml does not set", service, k, g))
		}
	}
	return d
}

// TestDurableSettingsEqualCompose: DurableSettings equals compose.yaml's declaration of
// durable-store, durable-schema and durable, field by field.
func TestDurableSettingsEqualCompose(t *testing.T) {
	services := readDurableCompose(t)
	if len(DurableServices) != 3 {
		t.Fatalf("DurableServices = %v, want the three services", DurableServices)
	}
	for _, name := range DurableServices {
		svc, ok := services[name]
		if !ok {
			t.Fatalf("compose.yaml has no service %s", name)
		}
		got, err := DurableSettings(name)
		if err != nil {
			t.Errorf("DurableSettings(%q): %v", name, err)
			continue
		}
		for _, d := range differences(name, got, wantDurable(t, svc)) {
			t.Error(d)
		}
		t.Logf("%s: compared %d environment values, %d tmpfs, entrypoint %v, health %q/%s", name, len(got.Env), len(got.Tmpfs), got.Entrypoint, got.HealthCmd, got.Healthy)
	}
}

// TestDurableSettingsComposeRead guards the reading: x-hardened's merge reaches each service, the
// values the comparison depends on are non-empty, and a `$$` is read as `$`.
func TestDurableSettingsComposeRead(t *testing.T) {
	services := readDurableCompose(t)
	for _, name := range DurableServices {
		s := services[name]
		w := wantDurable(t, s)
		if !s.ReadOnly || len(s.CapDrop) == 0 || len(s.SecurityOpt) == 0 {
			t.Errorf("%s does not carry x-hardened as read: %+v", name, s)
		}
		if w.User == "" || len(w.Tmpfs) == 0 || w.CPUs == "" || w.Memory == "" || w.Pids == 0 || len(w.Env) == 0 {
			t.Errorf("%s: a compared value is empty: %+v", name, w)
		}
	}
	if w := wantDurable(t, services["durable"]); len(w.Entrypoint) != 3 || !strings.Contains(w.Entrypoint[2], "POSTGRES_PWD=$(cat ") || strings.Contains(w.Entrypoint[2], "$$") {
		t.Errorf("durable's entrypoint did not have `$$` read as `$`: %q", w.Entrypoint)
	}
	if w := wantDurable(t, services["durable-store"]); w.HealthCmd != "pg_isready -U temporal -d temporal" || w.Healthy == "" {
		t.Errorf("durable-store's health check not read: %q %q", w.HealthCmd, w.Healthy)
	}
	if w := wantDurable(t, services["durable-schema"]); w.HealthCmd != "" {
		t.Errorf("durable-schema has no health check, read %q", w.HealthCmd)
	}
}

// TestDurableSettingsComparisonCatchesDifferences: every field's comparison fails on a planted
// difference, and passes on equal values (including the `no-new-privileges` spelling and the
// start period's spelling).
func TestDurableSettingsComparisonCatchesDifferences(t *testing.T) {
	services := readDurableCompose(t)
	for _, name := range DurableServices {
		want := wantDurable(t, services[name])
		got, err := DurableSettings(name)
		if err != nil {
			t.Fatal(err)
		}
		if d := differences(name, got, want); len(d) != 0 {
			t.Fatalf("clean case differs: %v", d)
		}
		mutations := map[string]func(s *Settings){
			"read-only":    func(s *Settings) { s.ReadOnly = !s.ReadOnly },
			"user":         func(s *Settings) { s.User += "1" },
			"cap_drop":     func(s *Settings) { s.CapDrop = nil },
			"security_opt": func(s *Settings) { s.SecurityOpt = []string{"seccomp=unconfined"} },
			"tmpfs":        func(s *Settings) { s.Tmpfs = append(append([]string{}, s.Tmpfs...), "/x") },
			"cpus":         func(s *Settings) { s.CPUs = "9" },
			"memory":       func(s *Settings) { s.Memory = "9g" },
			"pids":         func(s *Settings) { s.Pids++ },
			"entrypoint":   func(s *Settings) { s.Entrypoint = append(append([]string{}, s.Entrypoint...), "x") },
			"command":      func(s *Settings) { s.Command = []string{"x"} },
			"env missing":  func(s *Settings) { s.Env = map[string]string{} },
			"env different": func(s *Settings) {
				s.Env = map[string]string{}
				for k, v := range got.Env {
					s.Env[k] = v + "x"
				}
			},
			"env extra": func(s *Settings) {
				s.Env = map[string]string{"EXTRA": "1"}
				for k, v := range got.Env {
					s.Env[k] = v
				}
			},
			"health cmd":   func(s *Settings) { s.HealthCmd += " x" },
			"start period": func(s *Settings) { s.Healthy = "31s" },
		}
		for field, mutate := range mutations {
			if (field == "health cmd" || field == "start period") && want.HealthCmd == "" {
				continue // durable-schema has no health check
			}
			m := got
			m.Env = map[string]string{}
			for k, v := range got.Env {
				m.Env[k] = v
			}
			mutate(&m)
			if len(differences(name, m, want)) == 0 {
				t.Errorf("%s: a planted difference in %s was not caught", name, field)
			}
		}
		// Equal values, other spellings.
		m := got
		m.SecurityOpt = []string{"no-new-privileges"}
		if want.HealthCmd != "" {
			m.Healthy = "0m30s"
		}
		if d := differences(name, m, want); len(d) != 0 {
			t.Errorf("%s: equal values spelled differently were reported: %v", name, d)
		}
	}
}

// TestDurableSettingsUnresolvedVariable: a compose variable the comparison cannot resolve is
// reported, not silently read as empty.
func TestDurableSettingsUnresolvedVariable(t *testing.T) {
	r := &recorder{TB: t}
	svc := durableCompose{User: "${AP_HOST_USER}", Environment: yamlEnv{"A": "${NOPE:-x}"}}
	wantDurable(r, svc)
	if len(r.errs) != 2 {
		t.Errorf("reported %d unresolved variables, want 2: %v", len(r.errs), r.errs)
	}
}

// TestDurableSettingsParseForms: list and string forms of command, entrypoint and the health
// check test, and the list and map forms of environment, read the same.
func TestDurableSettingsParseForms(t *testing.T) {
	var s durableCompose
	src := "entrypoint: /bin/sh -c x\ncommand: []\nenvironment: [\"A=1\", \"B=\"]\nhealthcheck: {test: \"nc -z h 1\", start_period: 1m}\n"
	if err := yaml.Unmarshal([]byte(src), &s); err != nil {
		t.Fatal(err)
	}
	w := wantDurable(t, s)
	if !reflect.DeepEqual(w.Entrypoint, []string{"/bin/sh", "-c", "x"}) || len(w.Command) != 0 ||
		w.Env["A"] != "1" || w.Env["B"] != "" || len(w.Env) != 2 || w.HealthCmd != "nc -z h 1" || w.Healthy != "1m" {
		t.Errorf("forms read wrongly: %+v", w)
	}
	if healthCmd([]string{"CMD", "nc", "-z", "localhost", "7233"}) != "nc -z localhost 7233" || healthCmd([]string{"NONE"}) != "" {
		t.Error("healthCmd reads CMD or NONE wrongly")
	}
}

// TestDurableSettingsRefusesUnknownService: only the three durable services have settings.
func TestDurableSettingsRefusesUnknownService(t *testing.T) {
	for _, n := range []string{"", "engine", "check", "Durable", "durable "} {
		if _, err := DurableSettings(n); err == nil {
			t.Errorf("DurableSettings(%q) gave settings", n)
		}
	}
}
