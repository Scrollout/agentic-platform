//go:build tools

// Package vendortools lists the packages the generate step vendors besides those the generated
// code imports, so the later steps build offline: godog for the tests step, and the runtime
// packages generated code may import. It is used only while vendoring and is never written into
// the project.
package vendortools

import (
	_ "github.com/cucumber/godog"
	_ "github.com/cucumber/godog/colors"
	_ "github.com/oapi-codegen/runtime"
	_ "github.com/oapi-codegen/runtime/strictmiddleware/nethttp"
	_ "github.com/oapi-codegen/runtime/types"
)
