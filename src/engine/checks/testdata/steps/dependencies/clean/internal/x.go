package internal

import (
	"fmt"
	"net/http"

	"example.com/p/gen"
	"github.com/oapi-codegen/runtime/types"
)

var _, _, _, _ = fmt.Sprint, http.StatusOK, gen.X, types.Date{}
