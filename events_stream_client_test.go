package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStreamEventsDeliversFrames(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/events/stream" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: ready\ndata: {\"open_id\":\"ou_1\"}\n\n")
		fmt.Fprint(w, "data: {\"schema\":\"2.0\"}\n\n")
	}))
	defer srv.Close()

	c := NewGatewayClient(srv.URL)
	c.SetSessionToken("tok-stream")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var frames []sseFrame
	err := c.StreamEvents(ctx, func(event, data string) {
		frames = append(frames, sseFrame{event, data})
	})
	if err != nil {
		t.Fatalf("StreamEvents: %v", err)
	}
	if gotAuth != "Bearer tok-stream" {
		t.Fatalf("auth header = %q", gotAuth)
	}
	if len(frames) != 2 || frames[0].event != "ready" || frames[1].data != `{"schema":"2.0"}` {
		t.Fatalf("frames = %+v", frames)
	}
}

func TestStreamEventsAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, `{"code":"missing_session","message":"missing client token"}`)
	}))
	defer srv.Close()

	c := NewGatewayClient(srv.URL)
	err := c.StreamEvents(context.Background(), func(event, data string) {})
	if err == nil {
		t.Fatal("want error on 401")
	}
}
