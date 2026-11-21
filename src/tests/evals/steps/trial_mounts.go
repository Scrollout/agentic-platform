package steps

// trialMounts returns the docker -v values of a trial's agent container: the project it works on,
// writable, and the agent definitions, read-only. Nothing else of the host is mounted, so the
// held-out tests under testdata/ never reach the agent (FR-004).
func trialMounts(work, agents string) []string {
	return []string{work + ":/projects/p", agents + ":/agents:ro"}
}
