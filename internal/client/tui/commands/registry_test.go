package commands

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestRegistryDistinguishesCommandsFromMessages(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		text    string
		handled bool
		invalid bool
	}{
		{text: "hello"},
		{text: "/help\nexplain this text"},
		{text: "/help", handled: true},
		{text: "  /HELP \t", handled: true},
		{text: "/help unexpected", handled: true, invalid: true},
		{text: "/unknown", handled: true, invalid: true},
		{text: "/", handled: true, invalid: true},
	} {
		t.Run(test.text, func(t *testing.T) {
			calls := 0
			registry := New(Command{
				Name: "help", Description: "Show commands",
				Run: func() tea.Cmd {
					calls++

					return func() tea.Msg { return "executed" }
				},
			})

			command, handled, err := registry.Execute(test.text)
			if handled != test.handled || (err != nil) != test.invalid {
				t.Fatalf("handled = %v, error = %v", handled, err)
			}

			if !test.handled || test.invalid {
				if command != nil || calls != 0 {
					t.Fatal("unhandled or invalid input invoked a command")
				}

				return
			}

			if command == nil || calls != 1 || command() != "executed" {
				t.Fatal("registered command was not dispatched exactly once")
			}
		})
	}
}

func TestRegistryHelpUsesItsOwnCatalogue(t *testing.T) {
	t.Parallel()

	registry := New(
		Command{Name: "local", Description: "A client-specific action"},
		Command{Name: "other", Description: "Another action"},
	)

	help := registry.Help()
	if !strings.Contains(help, "/local") || !strings.Contains(help, "A client-specific action") {
		t.Fatalf("help omitted the registered command: %q", help)
	}

	if strings.Index(help, "/other") <= strings.Index(help, "/local") {
		t.Fatalf("help changed registration order: %q", help)
	}
}
