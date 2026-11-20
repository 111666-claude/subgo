package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func runCLI(t *testing.T, args ...string) (string, string, error) {
	t.Helper()

	// Reset the package-level flag values so tests do not leak into each other.
	outputFile = "output.srt"
	shiftAmount = 0
	stretch = 1.0
	clamp = true
	trimFirst = 0
	trimLast = 0
	trimBefore = 0
	trimAfter = 0
	removeHI = false
	removeIds = nil
	sliceRange = ""
	unsafeTags = "preserve"
	if err := rootCmd.Flags().Set("slice", ""); err != nil {
		t.Fatalf("reset slice flag: %v", err)
	}
	rootCmd.SetArgs(args)

	return captureOutput(t, func() error {
		return rootCmd.Execute()
	})
}

func captureOutput(t *testing.T, fn func() error) (string, string, error) {
	t.Helper()

	outFile, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatalf("stdout temp: %v", err)
	}
	errFile, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatalf("stderr temp: %v", err)
	}
	defer outFile.Close()
	defer errFile.Close()

	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outFile, errFile
	defer func() {
		os.Stdout, os.Stderr = oldOut, oldErr
	}()

	runErr := fn()

	if _, err := outFile.Seek(0, 0); err != nil {
		t.Fatalf("seek stdout: %v", err)
	}
	if _, err := errFile.Seek(0, 0); err != nil {
		t.Fatalf("seek stderr: %v", err)
	}
	out, _ := os.ReadFile(outFile.Name())
	errOut, _ := os.ReadFile(errFile.Name())
	return string(out), string(errOut), runErr
}

func TestParseSliceRange(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    time.Duration
		wantEnd time.Duration
		wantErr bool
	}{
		{name: "seconds", value: "1s-2s", want: time.Second, wantEnd: 2 * time.Second},
		{name: "mixed", value: "1m30s-2m", want: 90 * time.Second, wantEnd: 2 * time.Minute},
		{name: "spaces", value: " 5s - 10s ", want: 5 * time.Second, wantEnd: 10 * time.Second},
		{name: "missing separator", value: "5s", wantErr: true},
		{name: "bad start", value: "x-2s", wantErr: true},
		{name: "bad end", value: "1s-x", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, err := parseSliceRange(tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q", tt.value)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if start != tt.want || end != tt.wantEnd {
				t.Errorf("got %v-%v, want %v-%v", start, end, tt.want, tt.wantEnd)
			}
		})
	}
}

func TestCLISlice(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join("..", "..", "testdata", "fixtures", "cross_clip.ass")
	output := filepath.Join(dir, "clip.ass")

	_, stderr, err := runCLI(t, input, "--slice", "5s-10s", "--output", output)
	if err != nil {
		t.Fatalf("run: %v (stderr: %s)", err, stderr)
	}

	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	content := string(raw)
	for _, want := range []string{
		"Style: Caption,Arial,36",
		"Title: Cross-clip animation fixture",
		`{\move(500,100,900,100,0,5000)}Fly in`,
		`{\fade(0,0,255,0,500,1000,2000)}Fading in and out`,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("output missing %q:\n%s", want, content)
		}
	}
}

func TestCLISliceDiagnostics(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "gap.srt")
	if err := os.WriteFile(input, []byte("1\n00:00:01,000 --> 00:00:02,000\nHi\n\n"), 0o600); err != nil {
		t.Fatalf("write input: %v", err)
	}

	_, stderr, err := runCLI(t, input, "--slice", "10s-20s", "--output", filepath.Join(dir, "out.srt"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(stderr, "empty-slice") {
		t.Errorf("stderr = %q, want an empty-slice diagnostic", stderr)
	}
}

func TestCLISliceErrors(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "in.srt")
	if err := os.WriteFile(input, []byte("1\n00:00:01,000 --> 00:00:02,000\nHi\n\n"), 0o600); err != nil {
		t.Fatalf("write input: %v", err)
	}

	t.Run("reversed", func(t *testing.T) {
		_, _, err := runCLI(t, input, "--slice", "10s-5s", "--output", filepath.Join(dir, "out.srt"))
		if err == nil {
			t.Fatal("expected an error for a reversed range")
		}
		if !strings.Contains(err.Error(), "reversed-range") {
			t.Errorf("error = %v, want reversed-range", err)
		}
	})

	t.Run("bad unsafe policy", func(t *testing.T) {
		_, _, err := runCLI(t, input, "--slice", "0s-5s", "--unsafe-tags", "explode", "--output", filepath.Join(dir, "out.srt"))
		if err == nil {
			t.Fatal("expected an error for an invalid policy")
		}
	})

	t.Run("unsafe tag error policy", func(t *testing.T) {
		assInput := filepath.Join(dir, "karaoke.ass")
		content := "[Script Info]\n\n[V4+ Styles]\nFormat: Name, Fontname, Fontsize\nStyle: Default,Arial,20\n\n[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\nDialogue: 0,0:00:00.00,0:00:10.00,Default,,0,0,0,,{\\k50}Ka\n"
		if err := os.WriteFile(assInput, []byte(content), 0o600); err != nil {
			t.Fatalf("write input: %v", err)
		}
		_, _, err := runCLI(t, assInput, "--slice", "5s-8s", "--unsafe-tags", "error", "--output", filepath.Join(dir, "out.ass"))
		if err == nil {
			t.Fatal("expected an error for an unsafe tag")
		}
		if !strings.Contains(err.Error(), "unsafe-tag") || !strings.Contains(err.Error(), "event 1") {
			t.Errorf("error = %v, want a locatable unsafe-tag error", err)
		}
	})
}
