// Command check serves the check contract (src/check/api) in one of its two deployments
// (research D29): the check service, which runs the pattern's step checks over the projects folder
// it only reads and serves runCheck and readStepFiles, or one procedure step's worker, which runs
// that step's fixed procedure and writes its files (runProcedure). See package checksvc.
//
// It reads its settings from the environment only, all required: CHECK_LISTEN_ADDRESS (such as
// ":8080"); CHECK_TOKEN, the token this session issued to the engine for calling this deployment;
// CHECK_PROJECTS_DIR, the existing projects folder; CHECK_OAPI_CODEGEN, the pinned oapi-codegen
// binary; CHECK_TEMPLATE_DIR, the existing folder of the pattern's module template; and
// CHECK_DEPLOYMENT, `check-service` or `procedure-worker`. A procedure worker also needs
// CHECK_PROCEDURE, the procedure it serves (the step's worker is `procedure:<name>`), and
// CHECK_STEP, the step it serves; the check service refuses both.
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
var variables = []string{
	"CHECK_LISTEN_ADDRESS", "CHECK_TOKEN", "CHECK_PROJECTS_DIR", "CHECK_OAPI_CODEGEN", "CHECK_TEMPLATE_DIR", "CHECK_DEPLOYMENT",
}

// config reads the deployment's settings from getenv and checks them.
func config(getenv func(string) string) (checksvc.Config, string, error) {
	env := map[string]string{}
	for _, name := range variables {
		if env[name] = getenv(name); env[name] == "" {
			return checksvc.Config{}, "", fmt.Errorf("%s is not set", name)
		}
	}
	c := checksvc.Config{
		Token:       env["CHECK_TOKEN"],
		Project:     env["CHECK_PROJECTS_DIR"],
		OapiCodegen: env["CHECK_OAPI_CODEGEN"],
		Template:    env["CHECK_TEMPLATE_DIR"],
	}
	procedure, step := getenv("CHECK_PROCEDURE"), getenv("CHECK_STEP")
	switch env["CHECK_DEPLOYMENT"] {
	case "check-service":
		if procedure != "" || step != "" {
			return checksvc.Config{}, "", errors.New("the check service serves no procedure: unset CHECK_PROCEDURE and CHECK_STEP")
		}
	case "procedure-worker":
		if procedure == "" || step == "" {
			return checksvc.Config{}, "", errors.New("a procedure worker needs CHECK_PROCEDURE and CHECK_STEP")
		}
		c.Procedure, c.Step = procedure, step
	default:
		return checksvc.Config{}, "", fmt.Errorf("CHECK_DEPLOYMENT is %q, not check-service or procedure-worker", env["CHECK_DEPLOYMENT"])
	}
	for _, name := range []string{"CHECK_PROJECTS_DIR", "CHECK_TEMPLATE_DIR"} {
		if fi, err := os.Stat(env[name]); err != nil || !fi.IsDir() {
			return checksvc.Config{}, "", fmt.Errorf("%s is not an existing folder", name)
		}
	}
	if fi, err := os.Stat(c.OapiCodegen); err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm()&0o111 == 0 {
		return checksvc.Config{}, "", errors.New("CHECK_OAPI_CODEGEN is not an executable file")
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
