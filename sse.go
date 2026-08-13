package main

import (
	"bufio"
	"io"
	"strings"
)

// parseSSE reads a Server-Sent Events stream and invokes fn once per frame
// with the frame's event name ("" for plain data frames) and its data
// (multi-line data joined with \n). Comment lines (":") are ignored.
// Returns when the stream ends; io.EOF is not an error.
func parseSSE(r io.Reader, fn func(event, data string)) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	var event string
	var data []string
	flush := func() {
		if len(data) > 0 || event != "" {
			fn(event, strings.Join(data, "\n"))
		}
		event, data = "", nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, ":"):
			// comment / heartbeat
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	flush()
	return scanner.Err()
}
