package routes

import "testing"

func TestSessionRoutes(t *testing.T) {
	session := Session("session-1")
	if session.Name() != SessionName || session.Param(ParamSessionID) != "session-1" {
		t.Fatalf("Session() = %q/%q", session.Name(), session.Param(ParamSessionID))
	}

	sessions := Sessions("session-2")
	if sessions.Name() != SessionsName || sessions.Param(ParamSessionID) != "session-2" {
		t.Fatalf("Sessions() = %q/%q", sessions.Name(), sessions.Param(ParamSessionID))
	}

	inspection := Context("session-3")
	if inspection.Name() != ContextName || inspection.Param(ParamSessionID) != "session-3" {
		t.Fatalf("Context() = %q/%q", inspection.Name(), inspection.Param(ParamSessionID))
	}
}
