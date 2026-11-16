//go:build docker

package lab

import "strings"

// The step workers' tokens: lab constants made for the test installation, one pair per slice,
// step and project, never a session's. The worker token names the grant and registers the
// container; the call token is the one the engine calls the container with. Each differs from
// the lab's other tokens and from every other step's.
func stepWorkerToken(slice, step, project string) string {
	return "lab-step-worker-" + strings.ToLower(slice) + "-" + step + "-" + projectHash(project)
}

func stepCallToken(slice, step, project string) string {
	return "lab-step-call-" + strings.ToLower(slice) + "-" + step + "-" + projectHash(project)
}
