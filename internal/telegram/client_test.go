package telegram

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("request to secret URL failed")
}

func TestSendMessageTrackedReturnsMessageID(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"ok":true,"result":{"message_id":42,"chat":{"id":-100},"text":"test"}}`)),
			Header:     make(http.Header),
		}, nil
	})
	client := NewClient("token", &http.Client{Transport: transport})
	messageID, err := client.SendMessageTracked(context.Background(), -100, "test")
	if err != nil {
		t.Fatal(err)
	}
	if messageID != 42 {
		t.Fatalf("message id=%d, want 42", messageID)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestClientErrorDoesNotExposeToken(t *testing.T) {
	const token = "secret-token"
	client := NewClient(token, &http.Client{Transport: failingTransport{}})
	err := client.SendMessage(context.Background(), 1, "test")
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("error exposes token: %v", err)
	}
}
