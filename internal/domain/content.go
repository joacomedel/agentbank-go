package domain

// ForeignContent envuelve contenido escrito por otro participante (D20). El
// consumidor debe tratarlo como dato, nunca como instrucción.
type ForeignContent struct {
	Content   string `json:"content"`
	Untrusted bool   `json:"untrusted"`
	Source    string `json:"source,omitempty"`
}

// WrapForeign construye el envoltorio D20.
func WrapForeign(content, source string) ForeignContent {
	return ForeignContent{Content: content, Untrusted: true, Source: source}
}
