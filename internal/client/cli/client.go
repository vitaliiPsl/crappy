package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/event"
	"github.com/vitaliipsl/crappy/harness/session"
	"github.com/vitaliipsl/crappy/internal/runtime"
)

const prompt = "> "

type Client struct {
	runtime   *runtime.Runtime
	sessionID string
	input     io.Reader
	output    io.Writer
}

type input struct {
	text string
	err  error
}

func New(
	runtime *runtime.Runtime,
	sessionID string,
	input io.Reader,
	output io.Writer,
) *Client {
	return &Client{
		runtime:   runtime,
		sessionID: sessionID,
		input:     input,
		output:    output,
	}
}

func (c *Client) Run(ctx context.Context) error {
	subscription, err := c.runtime.Subscribe(ctx, c.sessionID, 0)
	if err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}
	defer subscription.Close()

	inputs := c.readInput(ctx)

	var (
		activeExecutionID string
		inputOpen         = true
		prompted          bool
	)

	for inputOpen || activeExecutionID != "" {
		if activeExecutionID == "" && inputOpen && !prompted {
			if _, err := io.WriteString(c.output, prompt); err != nil {
				return fmt.Errorf("write prompt: %w", err)
			}

			prompted = true
		}

		var availableInput <-chan input
		if activeExecutionID == "" {
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

			executionID, err := c.runtime.Execute(
				ctx,
				c.sessionID,
				kit.NewUserMessage(kit.NewTextContent(text)),
			)
			if err != nil {
				return fmt.Errorf("execute: %w", err)
			}

			activeExecutionID = executionID
		case value, ok := <-subscription.Events():
			if !ok {
				if err := subscription.Err(); err != nil {
					return fmt.Errorf("subscription: %w", err)
				}

				return nil
			}

			if value.Type == session.ExecutionStartedEntryType {
				activeExecutionID = value.ExecutionID
			}

			finishedExecutionID, err := c.handleEvent(value)
			if err != nil {
				return err
			}

			if finishedExecutionID != "" && finishedExecutionID == activeExecutionID {
				activeExecutionID = ""
			}
		}
	}

	return nil
}

func (c *Client) handleEvent(value event.Event) (string, error) {
	switch value.Type {
	case "agent.content_delta":
		var agentEvent kit.AgentEvent
		if err := json.Unmarshal(value.Payload, &agentEvent); err != nil {
			return "", fmt.Errorf("decode content delta: %w", err)
		}

		if agentEvent.Content == nil || agentEvent.Content.Text == nil {
			return "", nil
		}

		if _, err := io.WriteString(c.output, agentEvent.Content.Text.Text); err != nil {
			return "", fmt.Errorf("write content delta: %w", err)
		}
	case session.ExecutionFinishedEntryType:
		var finished session.ExecutionFinished
		if err := json.Unmarshal(value.Payload, &finished); err != nil {
			return "", fmt.Errorf("decode execution result: %w", err)
		}

		if _, err := fmt.Fprintln(c.output); err != nil {
			return "", fmt.Errorf("finish response: %w", err)
		}

		if finished.Outcome == session.ExecutionFailed {
			if _, err := fmt.Fprintf(c.output, "error: %s\n", finished.Error); err != nil {
				return "", fmt.Errorf("write execution error: %w", err)
			}
		}

		return value.ExecutionID, nil
	}

	return "", nil
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
