package automatic

const DefaultThresholdRatio = 0.70

type TriggerRequest struct {
	InputTokens int64
	InputLimit  int
}

type Trigger interface {
	ShouldTrigger(TriggerRequest) bool
}

type TriggerFunc func(TriggerRequest) bool

func (fn TriggerFunc) ShouldTrigger(request TriggerRequest) bool {
	return fn(request)
}

func WhenInputExceedsRatio(ratio float64) Trigger {
	return TriggerFunc(func(request TriggerRequest) bool {
		if ratio <= 0 || request.InputLimit <= 0 {
			return false
		}

		return float64(request.InputTokens) >= ratio*float64(request.InputLimit)
	})
}

func WhenInputExceedsTokens(tokens int64) Trigger {
	return TriggerFunc(func(request TriggerRequest) bool {
		return tokens > 0 && request.InputTokens >= tokens
	})
}

func Any(triggers ...Trigger) Trigger {
	return TriggerFunc(func(request TriggerRequest) bool {
		for _, trigger := range triggers {
			if trigger != nil && trigger.ShouldTrigger(request) {
				return true
			}
		}

		return false
	})
}
