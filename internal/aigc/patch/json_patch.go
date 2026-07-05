package patch

// JSONPatchOp is the patch subset used by text, storyboard, and UI events.
type JSONPatchOp struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value,omitempty"`
}
