package transcript

import (
	"strings"
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"
)

func TestDurableMessageReplacesStream(t *testing.T) {
	model := New()
	model.SetSize(80, 20)
	model.Apply(kit.NewAgentContentDeltaEvent(kit.NewTextContent("partial")))
	model.Append(kit.NewModelMessage(kit.NewTextContent("complete")))

	view := model.View()
	if !strings.Contains(view, "complete") || strings.Contains(view, "partial") {
		t.Fatalf("view = %q, want only durable response", view)
	}
}
