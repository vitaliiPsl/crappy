package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness"
	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/execution"
	"github.com/vitaliipsl/crappy/harness/session"
)

const prompt = "> "

type Client struct {
	harness   *harness.Harness
	sessionID string
	input     io.Reader
	output    io.Writer
}

type input struct {
	text string
	err  error
}

func New(
	harness *harness.Harness,
	sessionID string,
	input io.Reader,
	output io.Writer,
) *Client {
	return &Client{
		harness:   harness,
		sessionID: sessionID,
		input:     input,
		output:    output,
	}
}

func (c *Client) Run(ctx context.Context) error {
	opened, err := c.harness.Session(ctx, c.sessionID)
	if err != nil {
		return fmt.Errorf("open session: %w", err)
	}

	subscription, err := opened.Subscribe(ctx, 0)
	if err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}
	defer subscription.Close()

	inputs := c.readInput(ctx)
	events := subscription.Events()

	var (
		current   *session.Execution
		streamed  string
		inputOpen = true
		prompted  bool
	)

	for inputOpen || current != nil {
		if current == nil && inputOpen && !prompted {
			if _, err := io.WriteString(c.output, prompt); err != nil {
				return fmt.Errorf("write prompt: %w", err)
			}

			prompted = true
		}

		var availableInput <-chan input
		if current == nil {
			availableInput = inputs
		}

		var completed <-chan struct{}
		if current != nil {
			completed = current.Done()
		}

		select {
		case <-completed:
			result, err := current.Wait(ctx)
			if err != nil {
				return fmt.Errorf("wait execution: %w", err)
			}

			if err := c.handleResult(result, streamed); err != nil {
				return err
			}

			current = nil
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

			submitted, err := opened.Execute(ctx, execution.Request{
				Input: kit.NewUserMessage(kit.NewTextContent(text)),
			})
			if err != nil {
				return fmt.Errorf("execute: %w", err)
			}

			current = submitted
			streamed = ""
		case value, ok := <-events:
			if !ok {
				if err := subscription.Err(); err != nil {
					return fmt.Errorf("subscription: %w", err)
				}

				events = nil

				continue
			}

			if current == nil || value.ExecutionID != current.ID() {
				continue
			}

			text, err := c.handleEvent(value)
			if err != nil {
				return err
			}

			streamed += text
		}
	}

	return nil
}

func (c *Client) handleEvent(value event.Event) (string, error) {
	if value.Type != agent.EventType {
		return "", nil
	}

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

func (c *Client) handleResult(result execution.Result, streamed string) error {
	if len(result.Output) != 0 {
		var response kit.AgentResponse
		if err := json.Unmarshal(result.Output, &response); err != nil {
			return fmt.Errorf("decode agent result: %w", err)
		}

		var text strings.Builder
		for _, message := range response.Messages {
			if message.Role != kit.RoleModel {
				continue
			}

			for _, content := range message.Content {
				if content.Text != nil {
					text.WriteString(content.Text.Text)
				}
			}
		}

		if text.Len() == 0 && response.Output != nil {
			text.WriteString(response.Output.Text)
		}

		if completed := text.String(); strings.HasPrefix(completed, streamed) {
			if _, err := io.WriteString(c.output, completed[len(streamed):]); err != nil {
				return fmt.Errorf("write execution output: %w", err)
			}
		}
	}

	if _, err := fmt.Fprintln(c.output); err != nil {
		return fmt.Errorf("finish response: %w", err)
	}

	if result.Outcome == execution.Failed {
		if _, err := fmt.Fprintf(c.output, "error: %s\n", result.Error); err != nil {
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
