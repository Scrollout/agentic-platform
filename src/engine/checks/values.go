package checks

// The code step's value checks (catalogue README, "Declared values" and "Deployment values";
// ADR 0084, ADR 0085): a value the contract declares, or a value that changes with where the
// service runs, is never written as a literal in the step's own code.

// DeclaredValuesNotRepeated judges the step's own non-test .go files (matched by writable)
// against the approved contract's values: a literal that equals a declared number or string, or
// an enum value, fails, naming the file, the line, the value, the pointers that declare it and
// its Go home. approvedContract is the contract's YAML text.
func DeclaredValuesNotRepeated(project string, writable []string, approvedContract []byte) []CheckResult {
	return []CheckResult{fail("DeclaredValuesNotRepeated", "the check is not implemented")}
}

// DeploymentValuesConfigured judges the step's own non-test .go files (matched by writable): an
// address, a credential or a read of the environment fails, naming the file, the line and the
// kind, never the literal's text.
func DeploymentValuesConfigured(project string, writable []string) []CheckResult {
	return []CheckResult{fail("DeploymentValuesConfigured", "the check is not implemented")}
}
