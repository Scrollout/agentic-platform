package procedures

import (
	"context"
	"encoding/json"

	"github.com/agentic-platform/engine/checks"
)

// The contract-check step's files: where the approved contract and examples are copied.
const (
	contractPath = "api/openapi.yaml"
	examplesPath = "features/slice.feature"
)

// ContractCheck is the contract-check procedure. From the approved slice/contract and slice/rules
// parts it produces the outputs contract (api/openapi.yaml, the contract's text byte for byte) and
// examples (features/slice.feature, the rules part's examples byte for byte), in that order. It
// reads only the contract text and the examples. It refuses (Completed false) a wrong input set, a
// part that is not the JSON object it names, and a contract that is not exactly one YAML document
// without duplicate keys (checks.ContractUnreadable); the contract's lint is ContractLintPasses,
// which the engine runs on the copied file. A content that does not have its hash is
// ErrInputHashMismatch.
func ContractCheck(ctx context.Context, in []Input) (Result, error) {
	got, reason, err := takeInputs(in, "slice/contract", "slice/rules")
	if err != nil || reason != "" {
		return refused("%s", reason), err
	}
	var contract struct {
		Contract *string `json:"contract"`
	}
	if err := json.Unmarshal(got["slice/contract"].Content, &contract); err != nil || contract.Contract == nil {
		return refused("slice/contract is not the contract part, an object with the contract's text"), nil
	}
	var rules struct {
		Examples *string `json:"examples"`
	}
	if err := json.Unmarshal(got["slice/rules"].Content, &rules); err != nil || rules.Examples == nil {
		return refused("slice/rules is not the rules part, an object with the examples' text"), nil
	}
	if why := checks.ContractUnreadable(*contract.Contract); why != "" {
		return refused("the approved contract cannot be copied to %s: %s", contractPath, why), nil
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	return Result{
		Completed: true,
		Reason:    "copied the approved contract to " + contractPath + " and the approved examples to " + examplesPath,
		Outputs: []Output{
			output("contract", []File{{Path: contractPath, Content: []byte(*contract.Contract)}}),
			output("examples", []File{{Path: examplesPath, Content: []byte(*rules.Examples)}}),
		},
	}, nil
}
