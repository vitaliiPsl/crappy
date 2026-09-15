package automatic

import "testing"

func TestWhenInputExceedsRatio(t *testing.T) {
	t.Parallel()

	trigger := WhenInputExceedsRatio(0.8)

	for _, test := range []struct {
		name    string
		request TriggerRequest
		want    bool
	}{
		{name: "below", request: TriggerRequest{InputTokens: 79, InputLimit: 100}},
		{name: "at threshold", request: TriggerRequest{InputTokens: 80, InputLimit: 100}, want: true},
		{name: "above", request: TriggerRequest{InputTokens: 90, InputLimit: 100}, want: true},
		{name: "unknown limit", request: TriggerRequest{InputTokens: 90}, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := trigger.ShouldTrigger(test.request); got != test.want {
				t.Fatalf("ShouldTrigger() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestWhenInputExceedsTokens(t *testing.T) {
	t.Parallel()

	trigger := WhenInputExceedsTokens(100)

	if trigger.ShouldTrigger(TriggerRequest{InputTokens: 99}) {
		t.Fatal("ShouldTrigger() = true below threshold")
	}

	if !trigger.ShouldTrigger(TriggerRequest{InputTokens: 100}) {
		t.Fatal("ShouldTrigger() = false at threshold")
	}
}

func TestAny(t *testing.T) {
	t.Parallel()

	trigger := Any(
		WhenInputExceedsRatio(0.8),
		nil,
		WhenInputExceedsTokens(1000),
	)

	if !trigger.ShouldTrigger(TriggerRequest{InputTokens: 800, InputLimit: 1000}) {
		t.Fatal("ShouldTrigger() = false when ratio trigger matches")
	}

	if !trigger.ShouldTrigger(TriggerRequest{InputTokens: 1000}) {
		t.Fatal("ShouldTrigger() = false when token trigger matches")
	}

	if trigger.ShouldTrigger(TriggerRequest{InputTokens: 700, InputLimit: 1000}) {
		t.Fatal("ShouldTrigger() = true when no trigger matches")
	}
}
