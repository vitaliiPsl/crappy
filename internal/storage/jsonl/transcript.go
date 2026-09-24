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

func scanEntries(
	ctx context.Context,
	file *os.File,
	offset int64,
	visit func(entry.Entry, int64) bool,
) error {
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat transcript: %w", err)
	}

	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return fmt.Errorf("seek transcript: %w", err)
	}

	buffered := bufio.NewReader(file)
	completeBytes := offset

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		line, err := buffered.ReadBytes('\n')
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return fmt.Errorf("read transcript: %w", err)
		}

		current, err := decodeEntry(line, completeBytes)
		if err != nil {
			return err
		}

		if !visit(current, completeBytes) {
			return nil
		}

		completeBytes += int64(len(line))
	}

	if completeBytes == info.Size() {
		return nil
	}

	if err := file.Truncate(completeBytes); err != nil {
		return fmt.Errorf("repair transcript: %w", err)
	}

	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync repaired transcript: %w", err)
	}

	return nil
}

func encodeEntries(entries []entry.Entry) ([][]byte, error) {
	lines := make([][]byte, len(entries))

	for i, current := range entries {
		line, err := json.Marshal(current)
		if err != nil {
			return nil, err
		}

		lines[i] = append(line, '\n')
	}

	return lines, nil
}

func decodeEntry(
	line []byte,
	offset int64,
) (entry.Entry, error) {
	var current entry.Entry
	if err := json.Unmarshal(line, &current); err != nil {
		return entry.Entry{}, fmt.Errorf("decode transcript entry at offset %d: %w", offset, err)
	}

	return current, nil
}
