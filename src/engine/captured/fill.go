package captured

import (
	"fmt"

	"github.com/agentic-platform/engine/canonical"
)

// Fill gives files with every place of every parameter replaced by its value in values, in its
// case form, in contents and paths, longest source form first. An empty set fills to no file. It
// refuses two files that would get the same filled path, naming them.
//
// It also refuses, before filling anything, a parameter whose source value or whose value in
// values is not of its kind's form, a parameter with no value, and a value for no parameter.
// files is not changed.
func Fill(files []canonical.File, parameters []Parameter, values map[string]string) ([]canonical.File, error) {
	if err := checkSet(parameters); err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, p := range parameters {
		known[p.Name] = true
		v, ok := values[p.Name]
		if !ok {
			return nil, &Refused{Parameter: p.Name, Detail: "no value is given for it"}
		}
		if why := valueRefusal(p.Kind, v); why != "" {
			return nil, &Refused{Parameter: p.Name, Detail: why}
		}
	}
	for name := range values {
		if !known[name] {
			return nil, &Refused{Parameter: name, Detail: "a value is given for no parameter of that name"}
		}
	}
	if len(files) == 0 {
		return nil, nil
	}
	fill := filler(parameters, values)
	out := make([]canonical.File, 0, len(files))
	from := map[string]string{}
	for _, f := range files {
		path := fill(f.Path)
		if other, dup := from[path]; dup {
			return nil, fmt.Errorf("captured: the template files %s and %s both fill to the path %s", other, f.Path, path)
		}
		from[path] = f.Path
		out = append(out, canonical.File{Path: path, Content: []byte(fill(string(f.Content)))})
	}
	return out, nil
}
