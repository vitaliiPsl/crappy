package discovery

import (
	"testing"

	"github.com/vitaliiPsl/crappy-adk/kit"
	"github.com/vitaliiPsl/crappy-adk/kittest"
)

func TestToolSetKeepsUnexposedToolsRegistered(t *testing.T) {
	remote := kittest.NewTool(t, "remote", "")

	set := registeredSet(t, []kit.Tool{remote}, remote)
	if resolved, found := set.Lookup("remote"); !found || resolved != remote {
		t.Fatal("exposure changed registration")
	}

	if len(set.Tools()) != 2 {
		t.Fatal("remote tool was exposed before loading")
	}

	newTool := kittest.NewTool(t, "new_tool", "")
	if err := set.Register(newTool, remote); err == nil {
		t.Fatal("duplicate registration succeeded")
	}

	if _, found := set.Lookup("new_tool"); found {
		t.Fatal("failed batch partially registered tools")
	}

	if len(set.entries) != 1 {
		t.Fatal("failed registration changed the search catalog")
	}
}
