// Package gen is the server and types generated from the approved contract, api/openapi.yaml,
// and the stub (stub.go) that answers every operation with 501 Not Implemented. Never edit it by
// hand: the generate step writes it, and the GenerationRepeatable check runs this directive again
// and compares.
package gen

//go:generate oapi-codegen -config oapi-codegen.yaml ../api/openapi.yaml
