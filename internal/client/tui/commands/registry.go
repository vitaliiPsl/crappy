package commands

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

type Command struct {
	Name        string
	Description string
	Run         func() tea.Cmd
}

type Registry struct {
	commands []Command
}

func New(commands ...Command) *Registry {
	return &Registry{commands: append([]Command(nil), commands...)}
}

func (r *Registry) Execute(text string) (tea.Cmd, bool, error) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") || strings.ContainsRune(text, '\n') {
		return nil, false, nil
	}

	parts := strings.Fields(text[1:])
	if len(parts) == 0 {
		return nil, true, fmt.Errorf("enter a command after /; use /help to list commands")
	}

	name := strings.ToLower(parts[0])
	for _, command := range r.commands {
		if command.Name != name {
			continue
		}

		if len(parts) > 1 {
			return nil, true, fmt.Errorf("/%s does not accept arguments", name)
		}

		return command.Run(), true, nil
	}

	return nil, true, fmt.Errorf("unknown command /%s; use /help to list commands", name)
}

func (r *Registry) Help() string {
	var output strings.Builder
	for _, command := range r.commands {
		fmt.Fprintf(&output, "/%-8s %s\n", command.Name, command.Description)
	}

	return strings.TrimSuffix(output.String(), "\n")
}
