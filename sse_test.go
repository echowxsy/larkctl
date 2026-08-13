package main

import (
	"strings"
	"testing"
)

type sseFrame struct{ event, data string }

func collectSSE(t *testing.T, input string) []sseFrame {
	t.Helper()
	var frames []sseFrame
	err := parseSSE(strings.NewReader(input), func(event, data string) {
		frames = append(frames, sseFrame{event, data})
	})
	if err != nil {
		t.Fatalf("parseSSE: %v", err)
	}
	return frames
}

func TestParseSSEDataFrame(t *testing.T) {
	frames := collectSSE(t, "data: {\"a\":1}\n\n")
	if len(frames) != 1 || frames[0].event != "" || frames[0].data != `{"a":1}` {
		t.Fatalf("frames = %+v", frames)
	}
}

func TestParseSSENamedEvent(t *testing.T) {
	frames := collectSSE(t, "event: ready\ndata: {\"open_id\":\"ou_1\"}\n\n")
	if len(frames) != 1 || frames[0].event != "ready" || frames[0].data != `{"open_id":"ou_1"}` {
		t.Fatalf("frames = %+v", frames)
	}
}

func TestParseSSEIgnoresComments(t *testing.T) {
	frames := collectSSE(t, ": ping\n\ndata: x\n\n: ping\n\n")
	if len(frames) != 1 || frames[0].data != "x" {
		t.Fatalf("frames = %+v", frames)
	}
}

func TestParseSSEMultiLineData(t *testing.T) {
	frames := collectSSE(t, "data: line1\ndata: line2\n\n")
	if len(frames) != 1 || frames[0].data != "line1\nline2" {
		t.Fatalf("frames = %+v", frames)
	}
}

func TestParseSSEMultipleFrames(t *testing.T) {
	frames := collectSSE(t, "event: ready\ndata: r\n\ndata: e1\n\ndata: e2\n\n")
	if len(frames) != 3 {
		t.Fatalf("frames = %+v", frames)
	}
	if frames[1] != (sseFrame{"", "e1"}) || frames[2] != (sseFrame{"", "e2"}) {
		t.Fatalf("frames = %+v", frames)
	}
}
