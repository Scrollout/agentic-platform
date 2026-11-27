module github.com/agentic-platform/cli

go 1.26

require (
	github.com/agentic-platform/retry v0.0.0
	github.com/oapi-codegen/runtime v1.8.0
	go.yaml.in/yaml/v3 v3.0.6
)

require (
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
)

replace github.com/agentic-platform/retry => ../retry
