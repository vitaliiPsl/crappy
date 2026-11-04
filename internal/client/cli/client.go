package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/session/state"
	"github.com/vitaliipsl/crappy/internal/assistant"
)

const prompt = "> "

type Client struct {
	assistant *assistant.Assistant
	sessionID string
	input     io.Reader
	output    io.Writer
}

type input struct {
	text string
	err  error
}

func New(
	app *assistant.Assistant,
	sessionID string,
	input io.Reader,
	output io.Writer,
) *Client {
	return &Client{
		assistant: app,
		sessionID: sessionID,
		input:     input,
		output:    output,
	}
}

func (c *Client) Run(ctx context.Context) error {
	var (
		opened state.Record
		err    error
	)

	if c.sessionID == "" {
		opened, err = c.assistant.CreateSession(ctx, "")
	} else {
		opened, err = c.assistant.GetSession(ctx, c.sessionID)
	}

	if err != nil {
		return fmt.Errorf("open session: %w", err)
	}

	subscription, err := c.assistant.Subscribe(ctx, opened.ID, 0)
	if err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}
	defer subscription.Close()

	inputs := c.readInput(ctx)
	events := subscription.Events()

	var (
		executionID string
		streamed    string
		inputOpen   = true
		prompted    bool
	)

	for inputOpen || executionID != "" {
		if executionID == "" && inputOpen && !prompted {
			if _, err := io.WriteString(c.output, prompt); err != nil {
				return fmt.Errorf("write prompt: %w", err)
			}

			prompted = true
		}

		var availableInput <-chan input
		if executionID == "" {
			availableInput = inputs
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case value, ok := <-availableInput:
			if !ok {
				inputOpen = false

				continue
			}

			if value.err != nil {
				return fmt.Errorf("read input: %w", value.err)
			}

			prompted = false

			text := strings.TrimSpace(value.text)
			if text == "" {
				continue
			}

			submitted, err := c.assistant.Execute(ctx, opened.ID, kit.NewTextContent(text))
			if err != nil {
				return fmt.Errorf("execute: %w", err)
			}

			executionID = submitted
			streamed = ""
		case value, ok := <-events:
			if !ok {
				if err := subscription.Err(); err != nil {
					return fmt.Errorf("subscription: %w", err)
				}

				return ctx.Err()
			}

			if executionID == "" || value.ExecutionID != executionID {
				continue
			}

			switch value.Type {
			case agent.EventType:
				text, err := c.writeDelta(value)
				if err != nil {
					return err
				}

				streamed += text
			case agent.MessageEntryType:
				if err := c.writeUnstreamed(value, streamed); err != nil {
					return err
				}

				streamed = ""
			case execution.EntryType:
				var record execution.Record
				if err := json.Unmarshal(value.Payload, &record); err != nil {
					return fmt.Errorf("decode execution: %w", err)
				}

				if !record.Status.Terminal() {
					continue
				}

				if err := c.finish(record); err != nil {
					return err
				}

				executionID = ""
			}
		}
	}

	return nil
}

func (c *Client) writeDelta(value event.Event) (string, error) {
	var agentEvent kit.AgentEvent
	if err := json.Unmarshal(value.Payload, &agentEvent); err != nil {
		return "", fmt.Errorf("decode agent event: %w", err)
	}

	if agentEvent.Type != kit.EventContentDelta ||
		agentEvent.Content == nil ||
		agentEvent.Content.Text == nil {
		return "", nil
	}

	text := agentEvent.Content.Text.Text
	if _, err := io.WriteString(c.output, text); err != nil {
		return "", fmt.Errorf("write content delta: %w", err)
	}

	return text, nil
}

func (c *Client) writeUnstreamed(value event.Event, streamed string) error {
	var message kit.Message
	if err := json.Unmarshal(value.Payload, &message); err != nil {
		return fmt.Errorf("decode agent message: %w", err)
	}

	if message.Role != kit.RoleModel {
		return nil
	}

	var text strings.Builder
	for _, content := range message.Content {
		if content.Text != nil {
			text.WriteString(content.Text.Text)
		}
	}

	if completed := text.String(); strings.HasPrefix(completed, streamed) {
		if _, err := io.WriteString(c.output, completed[len(streamed):]); err != nil {
			return fmt.Errorf("write agent message: %w", err)
		}
	}

	return nil
}

func (c *Client) finish(record execution.Record) error {
	if _, err := fmt.Fprintln(c.output); err != nil {
		return fmt.Errorf("finish response: %w", err)
	}

	if record.Status == execution.Failed {
		if _, err := fmt.Fprintf(c.output, "error: %s\n", record.Error); err != nil {
			return fmt.Errorf("write execution error: %w", err)
		}
	}

	return nil
}

func (c *Client) readInput(ctx context.Context) <-chan input {
	inputs := make(chan input)
	scanner := bufio.NewScanner(c.input)

	go func() {
		defer close(inputs)

		for scanner.Scan() {
			if !sendInput(ctx, inputs, input{text: scanner.Text()}) {
				return
			}
		}

		if err := scanner.Err(); err != nil {
			sendInput(ctx, inputs, input{err: err})
		}
	}()

	return inputs
}

func sendInput(ctx context.Context, inputs chan<- input, value input) bool {
	select {
	case inputs <- value:
		return true
	case <-ctx.Done():
		return false
	}
}
