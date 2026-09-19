package jsonl

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/vitaliipsl/crappy/harness/entry"
)

func loadEntries(
	ctx context.Context,
	file *os.File,
) ([]entry.Entry, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat transcript: %w", err)
	}

	entries := make([]entry.Entry, 0)
	buffered := bufio.NewReader(file)
	lineNumber := 0
	completeBytes := int64(0)

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		line, err := buffered.ReadBytes('\n')
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("read transcript: %w", err)
		}

		lineNumber++

		current, err := decodeEntry(line, lineNumber)
		if err != nil {
			return nil, err
		}

		entries = append(entries, current)
		completeBytes += int64(len(line))
	}

	if completeBytes == info.Size() {
		return entries, nil
	}

	if err := file.Truncate(completeBytes); err != nil {
		return nil, fmt.Errorf("repair transcript: %w", err)
	}

	if err := file.Sync(); err != nil {
		return nil, fmt.Errorf("sync repaired transcript: %w", err)
	}

	return entries, nil
}

func encodeEntries(entries []entry.Entry) ([]byte, error) {
	var encoded []byte

	for _, current := range entries {
		line, err := json.Marshal(current)
		if err != nil {
			return nil, err
		}

		encoded = append(encoded, line...)
		encoded = append(encoded, '\n')
	}

	return encoded, nil
}

func decodeEntry(
	line []byte,
	lineNumber int,
) (entry.Entry, error) {
	var current entry.Entry
	if err := json.Unmarshal(line, &current); err != nil {
		return entry.Entry{}, fmt.Errorf("decode transcript line %d: %w", lineNumber, err)
	}

	return current, nil
}
