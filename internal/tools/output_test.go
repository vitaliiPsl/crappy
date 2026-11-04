package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/vitaliiPsl/crappy-adk/kit"

	"github.com/vitaliipsl/crappy/harness/agent"
	"github.com/vitaliipsl/crappy/harness/session/state"
	"github.com/vitaliipsl/crappy/internal/storage/jsonl"
	"github.com/vitaliipsl/crappy/internal/tools/filesystem"
)

const testOutputLimit = 1024

func TestOutputCapStoresFullTextForReadFile(t *testing.T) {
	t.Parallel()

	middleware, root := newTestOutputCap(t)
	value := numberedLines(400)

	output, err := executeWithOutputCap(t, middleware, kit.NewToolOutput(kit.NewTextContent(value)), nil)
	if err != nil {
		t.Fatal(err)
	}

	preview := output.Content[0].Text.Text
	if len(preview) > testOutputLimit || !strings.HasPrefix(preview, "line 000\n") || !strings.HasSuffix(preview, "line 399") {
		t.Fatalf("preview does not preserve bounded head and tail: %q", preview)
	}

	path := assertFullOutputBlob(t, root, preview, value)
	if !strings.Contains(preview, "use read_file with start/end to inspect") {
		t.Fatalf("preview has no retrieval instructions: %q", preview)
	}

	read := filesystem.NewReadFile(t.TempDir())

	selected, err := read.Execute(kit.NewRunContext(context.Background()), kit.NewToolCall("read", "read_file", map[string]any{
		"path": path, "start": 201, "end": 201,
	}))
	if err != nil || selected.Content[0].Text.Text != "line 200" {
		t.Fatalf("read omitted output = %+v, error = %v", selected, err)
	}
}

func TestOutputCapPreservesRichContentAndOriginalOutput(t *testing.T) {
	t.Parallel()

	middleware, _ := newTestOutputCap(t)
	first := numberedLines(400)
	second := strings.Repeat("second output\n", 200)
	image := kit.NewImageContent("image/png", []byte("image"))
	input := kit.NewToolOutput(kit.NewTextContent(first), image, kit.NewTextContent(second))
	input.Structured = map[string]any{"status": "done"}

	output, err := executeWithOutputCap(t, middleware, input, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(output.Content) != 3 || !reflect.DeepEqual(output.Content[1], image) || !reflect.DeepEqual(output.Structured, input.Structured) {
		t.Fatalf("rich output changed: %+v", output)
	}

	for _, index := range []int{0, 2} {
		preview := output.Content[index].Text.Text
		if len(preview) > testOutputLimit || !strings.Contains(preview, "full output:") {
			t.Fatalf("content %d was not capped: %q", index, preview)
		}
	}

	if input.Content[0].Text.Text != first || input.Content[2].Text.Text != second {
		t.Fatal("capping mutated the original output")
	}
}

func TestOutputCapPreservesErrorIdentity(t *testing.T) {
	t.Parallel()

	middleware, root := newTestOutputCap(t)
	original := &os.PathError{Op: "read", Path: numberedLines(400), Err: os.ErrPermission}

	_, err := executeWithOutputCap(t, middleware, kit.ToolOutput{}, original)
	if err == nil || len(err.Error()) > testOutputLimit || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("capped error = %v", err)
	}

	var pathErr *os.PathError
	if !errors.As(err, &pathErr) || pathErr != original {
		t.Fatal("capping lost the original error")
	}

	assertFullOutputBlob(t, root, err.Error(), original.Error())
}

func TestOutputCapLeavesSmallAndNonTextResultsUnchanged(t *testing.T) {
	t.Parallel()

	middleware := OutputCap(Config{MaxOutputBytes: testOutputLimit}, func(context.Context, string, []byte) (string, error) {
		t.Fatal("small or non-text output was stored")

		return "", nil
	})
	smallErr := errors.New("small error")

	for _, output := range []kit.ToolOutput{
		kit.NewToolOutput(kit.NewTextContent("small")),
		kit.NewToolOutput(kit.NewImageContent("image/png", make([]byte, 2048))),
		kit.NewStructuredToolOutput(map[string]any{"large": strings.Repeat("x", 2048)}),
	} {
		got, err := executeWithOutputCap(t, middleware, output, smallErr)
		if err != smallErr || !reflect.DeepEqual(got, output) {
			t.Fatalf("small result changed: %+v, %v", got, err)
		}
	}
}

func TestOutputCapReportsStorageFailuresWithoutLosingToolError(t *testing.T) {
	t.Parallel()

	storageErr := errors.New("storage unavailable")
	middleware := OutputCap(Config{MaxOutputBytes: testOutputLimit}, func(context.Context, string, []byte) (string, error) {
		return "", storageErr
	})
	original := errors.New(numberedLines(400))

	for _, output := range []kit.ToolOutput{{}, kit.NewToolOutput(kit.NewTextContent(numberedLines(400)))} {
		_, err := executeWithOutputCap(t, middleware, output, original)
		if !errors.Is(err, storageErr) || !errors.Is(err, original) || len(err.Error()) > testOutputLimit {
			t.Fatalf("storage failure lost error identity or exposed full output: %v", err)
		}
	}
}

func TestOutputCapPreservesCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	middleware := OutputCap(Config{MaxOutputBytes: testOutputLimit}, func(context.Context, string, []byte) (string, error) {
		t.Fatal("cancelled output was stored")

		return "", nil
	})
	original := fmt.Errorf("%s: %w", numberedLines(400), context.Canceled)

	wrapped, err := middleware(agent.BuildContext{}, testTool{
		definition: kit.ToolDefinition{Name: "test"},
		execute: func(*kit.RunContext, kit.ToolCall) (kit.ToolOutput, error) {
			return kit.ToolOutput{}, original
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := wrapped.Execute(kit.NewRunContext(ctx), kit.NewToolCall("call", "test", nil)); err != original {
		t.Fatalf("cancellation changed: %v", err)
	}
}

func TestOutputCapStoresOutsideToolTimeout(t *testing.T) {
	t.Parallel()

	var toolContext context.Context

	config := Config{Timeout: time.Minute, MaxTimeout: time.Minute, MaxOutputBytes: testOutputLimit}

	timed, err := Timeout(config)(agent.BuildContext{}, testTool{
		definition: kit.ToolDefinition{Name: "test"},
		execute: func(run *kit.RunContext, _ kit.ToolCall) (kit.ToolOutput, error) {
			toolContext = run.Context

			return kit.NewToolOutput(kit.NewTextContent(numberedLines(400))), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	middleware := OutputCap(config, func(ctx context.Context, _ string, _ []byte) (string, error) {
		if ctx.Err() != nil || !errors.Is(toolContext.Err(), context.Canceled) {
			t.Fatal("storage must use the parent context after the tool context closes")
		}

		return "/full-output", nil
	})

	wrapped, err := middleware(agent.BuildContext{}, timed)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := wrapped.Execute(kit.NewRunContext(context.Background()), kit.NewToolCall("call", "test", nil)); err != nil {
		t.Fatal(err)
	}
}

func TestCappedPreviewPreservesUTF8AndLimit(t *testing.T) {
	t.Parallel()

	for _, value := range []string{strings.Repeat("🙂", 600), strings.Repeat("x\xff", 2000)} {
		preview, err := cappedPreview(value, "/full-output", testOutputLimit)
		if err != nil {
			t.Fatal(err)
		}

		if !utf8.ValidString(preview) || len(preview) > testOutputLimit {
			t.Fatalf("invalid or oversized preview: %q", preview)
		}
	}
}

func executeWithOutputCap(t *testing.T, middleware agent.ToolMiddleware, output kit.ToolOutput, toolErr error) (kit.ToolOutput, error) {
	t.Helper()

	wrapped, err := middleware(agent.BuildContext{Request: agent.BuildRequest{SessionID: "session"}}, testTool{
		definition: kit.ToolDefinition{Name: "test"},
		execute: func(*kit.RunContext, kit.ToolCall) (kit.ToolOutput, error) {
			return output, toolErr
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	return wrapped.Execute(kit.NewRunContext(context.Background()), kit.NewToolCall("call", "test", nil))
}

func newTestOutputCap(t *testing.T) (agent.ToolMiddleware, string) {
	t.Helper()

	root := t.TempDir()

	storage, err := jsonl.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	if err := storage.SessionStore().Save(context.Background(), state.Record{ID: "session"}); err != nil {
		t.Fatal(err)
	}

	return OutputCap(Config{MaxOutputBytes: testOutputLimit}, storage.PutBlob), root
}

func assertFullOutputBlob(t *testing.T, root, preview, want string) string {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join(root, "session", "blobs", "*"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("blob paths = %v, error = %v; want one full output", paths, err)
	}

	path := paths[0]
	if !strings.Contains(preview, path) {
		t.Fatalf("preview does not reference %q: %q", path, preview)
	}

	data, err := os.ReadFile(path)
	if err != nil || string(data) != want {
		t.Fatalf("full output differs: error = %v", err)
	}

	return path
}

func numberedLines(count int) string {
	lines := make([]string, count)
	for index := range lines {
		lines[index] = fmt.Sprintf("line %03d", index)
	}

	return strings.Join(lines, "\n")
}
