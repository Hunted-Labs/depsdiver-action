package template

import (
	"fmt"
	"strings"
	"text/template"
)

func RegisterHelpers(funcMap template.FuncMap) template.FuncMap {
	if funcMap == nil {
		funcMap = make(template.FuncMap)
	}

	funcMap["upper"] = strings.ToUpper
	funcMap["lower"] = strings.ToLower
	funcMap["title"] = strings.Title
	funcMap["join"] = strings.Join
	funcMap["split"] = strings.Split
	funcMap["fmt"] = fmt.Sprintf

	return funcMap
}

func DefaultFuncMap() template.FuncMap {
	return RegisterHelpers(nil)
}

