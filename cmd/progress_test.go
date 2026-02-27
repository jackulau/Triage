package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestProgressBar(t *testing.T) {
	var buf bytes.Buffer
	bar := newProgressBar(10, "Testing", &buf)

	bar.Add(5)
	output := buf.String()

	if !strings.Contains(output, "Testing") {
		t.Errorf("progress bar output should contain description, got %q", output)
	}
	if !strings.Contains(output, "5/10") {
		t.Errorf("progress bar output should contain count, got %q", output)
	}
	if !strings.Contains(output, "[") || !strings.Contains(output, "]") {
		t.Errorf("progress bar output should contain brackets, got %q", output)
	}
}

func TestProgressBarFinish(t *testing.T) {
	var buf bytes.Buffer
	bar := newProgressBar(5, "Done", &buf)

	bar.Add(3)
	bar.Finish()
	output := buf.String()

	if !strings.Contains(output, "5/5") {
		t.Errorf("finished progress bar should show total/total, got %q", output)
	}
	if !strings.HasSuffix(output, "\n") {
		t.Errorf("finished progress bar should end with newline, got %q", output)
	}
}

func TestProgressBarZeroTotal(t *testing.T) {
	var buf bytes.Buffer
	bar := newProgressBar(0, "Empty", &buf)

	bar.Add(1)
	bar.Finish()

	// Should not panic and should produce no output (render skips when total <= 0)
	// The Finish() call writes a newline
}

func TestProgressBarOverflow(t *testing.T) {
	var buf bytes.Buffer
	bar := newProgressBar(5, "Overflow", &buf)

	bar.Add(10) // More than total
	output := buf.String()

	// Current should be capped at total
	if !strings.Contains(output, "5/5") {
		t.Errorf("overflowed progress bar should cap at total, got %q", output)
	}
}

func TestProgressBarRender(t *testing.T) {
	var buf bytes.Buffer
	bar := newProgressBar(4, "Render", &buf)

	bar.Add(2) // 50%
	output := buf.String()

	// 50% of 30 width = 15 '=' characters
	equalCount := strings.Count(output, "=")
	if equalCount != 15 {
		t.Errorf("at 50%% should have 15 '=' chars, got %d in %q", equalCount, output)
	}
}
