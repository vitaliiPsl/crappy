package session

import harnesssession "github.com/vitaliipsl/crappy/harness/session"

type noticeKind uint8

const (
	noticeNone noticeKind = iota
	noticeError
	noticeInterrupted
)

type notice struct {
	kind    noticeKind
	message string
}

func errorNotice(err error) notice {
	return notice{kind: noticeError, message: err.Error()}
}

func executionNotice(finished harnesssession.ExecutionFinished) notice {
	switch finished.Outcome {
	case harnesssession.ExecutionFailed:
		return notice{kind: noticeError, message: finished.Error}
	case harnesssession.ExecutionCancelled, harnesssession.ExecutionInterrupted:
		return notice{kind: noticeInterrupted}
	}

	return notice{}
}
