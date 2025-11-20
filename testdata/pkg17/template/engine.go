package template

import (
	"bytes"
	"errors"
	"text/template"
)

var ErrTemplateNotFound = errors.New("template not found")

type Engine struct {
	templates map[string]*template.Template
}

func NewEngine() *Engine {
	return &Engine{
		templates: make(map[string]*template.Template),
	}
}

func (e *Engine) Register(name, content string) error {
	tmpl, err := template.New(name).Parse(content)
	if err != nil {
		return err
	}
	e.templates[name] = tmpl
	return nil
}

func (e *Engine) Render(name string, data interface{}) (string, error) {
	tmpl, exists := e.templates[name]
	if !exists {
		return "", ErrTemplateNotFound
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

