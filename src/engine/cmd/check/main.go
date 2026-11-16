// Command check serves the check contract (src/check/api) in one of its three deployments
// (research D29, D30): the check service, which runs the pattern's step checks over the projects
// folder it only reads and serves runCheck and readStepFiles; one procedure step's worker, which
// runs that step's fixed procedure and writes its files (runProcedure); or the project worker,
// which keeps each project's version history (commitStep, restoreStep). See package checksvc.
//
// It reads its settings from the environment only. Every deployment needs CHECK_LISTEN_ADDRESS
// (such as ":8080"); CHECK_TOKEN, the token this session issued to the engine for calling this
// deployment; CHECK_PROJECTS_DIR, the existing projects folder; and CHECK_DEPLOYMENT,
// `check-service`, `procedure-worker` or `project-worker`. The check service and a procedure
// worker also need CHECK_OAPI_CODEGEN, the pinned oapi-codegen binary, and CHECK_TEMPLATE_DIR, the
// existing folder of the pattern's module template, and may name in CHECK_GOCACHE_BASE the
// read-only warm base Go cache each call copies as its GOCACHE (the image sets /opt/gocache-base;
// unset, each call starts from an empty cache; set but not a folder, every call fails). A procedure worker also needs CHECK_PROCEDURE,
// the procedure it serves (the step's worker is `procedure:<name>`), and CHECK_STEP, the step it
// serves; the other deployments refuse both. The project worker needs CHECK_HISTORY_DIR, the
// existing folder, outside every project, where it keeps its record of each project's last
// commit, and may name its git binary in CHECK_GIT (default: git on PATH); the other deployments
// refuse both.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/agentic-platform/engine/checksvc"
)

func main() {
	if err := run(); err != nil {
		log.Fatal("check: ", err)
	}
}

// variables are the settings every deployment needs.
var variables = []string{"CHECK_LISTEN_ADDRESS", "CHECK_TOKEN", "CHECK_PROJECTS_DIR", "CHECK_DEPLOYMENT"}

// config reads the deployment's settings from getenv and checks them.
func config(getenv func(string) string) (checksvc.Config, string, error) {
	env := map[string]string{}
	for _, name := range variables {
		if env[name] = getenv(name); env[name] == "" {
			return checksvc.Config{}, "", fmt.Errorf("%s is not set", name)
		}
	}
	c := checksvc.Config{Token: env["CHECK_TOKEN"], Project: env["CHECK_PROJECTS_DIR"]}
	procedure, step := getenv("CHECK_PROCEDURE"), getenv("CHECK_STEP")
	history, git := getenv("CHECK_HISTORY_DIR"), getenv("CHECK_GIT")
	folders := []string{"CHECK_PROJECTS_DIR"}
	tools := true
	switch env["CHECK_DEPLOYMENT"] {
	case "check-service":
		if procedure != "" || step != "" || history != "" || git != "" {
			return checksvc.Config{}, "", errors.New("the check service serves no procedure and keeps no version history: unset CHECK_PROCEDURE, CHECK_STEP, CHECK_HISTORY_DIR and CHECK_GIT")
		}
	case "procedure-worker":
		if procedure == "" || step == "" {
			return checksvc.Config{}, "", errors.New("a procedure worker needs CHECK_PROCEDURE and CHECK_STEP")
		}
		if history != "" || git != "" {
			return checksvc.Config{}, "", errors.New("a procedure worker keeps no version history: unset CHECK_HISTORY_DIR and CHECK_GIT")
		}
		c.Procedure, c.Step = procedure, step
	case "project-worker":
		if procedure != "" || step != "" {
			return checksvc.Config{}, "", errors.New("the project worker serves no procedure: unset CHECK_PROCEDURE and CHECK_STEP")
		}
		if history == "" {
			return checksvc.Config{}, "", errors.New("the project worker needs CHECK_HISTORY_DIR")
		}
		c.History, c.Git = history, git
		env["CHECK_HISTORY_DIR"] = history
		folders = append(folders, "CHECK_HISTORY_DIR")
		tools = false
	default:
		return checksvc.Config{}, "", fmt.Errorf("CHECK_DEPLOYMENT is %q, not check-service, procedure-worker or project-worker", env["CHECK_DEPLOYMENT"])
	}
	if tools {
		for _, name := range []string{"CHECK_OAPI_CODEGEN", "CHECK_TEMPLATE_DIR"} {
			if env[name] = getenv(name); env[name] == "" {
				return checksvc.Config{}, "", fmt.Errorf("%s is not set", name)
			}
		}
		c.OapiCodegen, c.Template = env["CHECK_OAPI_CODEGEN"], env["CHECK_TEMPLATE_DIR"]
		c.GoCacheBase = getenv("CHECK_GOCACHE_BASE")
		folders = append(folders, "CHECK_TEMPLATE_DIR")
		if fi, err := os.Stat(c.OapiCodegen); err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm()&0o111 == 0 {
			return checksvc.Config{}, "", errors.New("CHECK_OAPI_CODEGEN is not an executable file")
		}
	} else if c.Git != "" {
		if fi, err := os.Stat(c.Git); err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm()&0o111 == 0 {
			return checksvc.Config{}, "", errors.New("CHECK_GIT is not an executable file")
		}
	}
	for _, name := range folders {
		if fi, err := os.Stat(env[name]); err != nil || !fi.IsDir() {
			return checksvc.Config{}, "", fmt.Errorf("%s is not an existing folder", name)
		}
	}
	return c, env["CHECK_LISTEN_ADDRESS"], nil
}

func run() error {
	c, addr, err := config(os.Getenv)
	if err != nil {
		return err
	}
	// On SIGTERM or an interrupt, the server stops after answering the calls it is serving.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	srv := &http.Server{
		Addr:              addr,
		Handler:           checksvc.NewHandler(c),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       time.Minute,
		// A call may run for the contract's call-time-limit-seconds-max (600 s), then answer.
		WriteTimeout: 11 * time.Minute,
		IdleTimeout:  2 * time.Minute,
	}
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	<-drained
	return nil
}
