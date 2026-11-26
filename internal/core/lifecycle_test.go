package core

import (
	"context"
	"testing"
	"time"
)

type closingProvider struct {
	instrumentedProvider
	entered, cleaned chan struct{}
}

func (p *closingProvider) Extract(ctx context.Context, r ExtractRequest) (ExtractResponse, error) {
	close(p.entered)
	<-ctx.Done()
	time.Sleep(20 * time.Millisecond)
	close(p.cleaned)
	return ExtractResponse{}, ctx.Err()
}
func TestServiceCloseCancelsDetachedExtractionAndWaitsForCleanup(t *testing.T) {
	p := &closingProvider{instrumentedProvider: instrumentedProvider{name: "closing"}, entered: make(chan struct{}), cleaned: make(chan struct{})}
	reg := NewRegistry()
	if err := reg.Register(p); err != nil {
		t.Fatal(err)
	}
	svc := NewService(reg, freeTierLedger(p.Name(), 10), RouteMatrix{TaskExtract: {p.Name()}})
	ctx, cancel := context.WithCancel(context.Background())
	returned := make(chan struct{})
	go func() { svc.Extract(ctx, ExtractRequest{URL: "https://8.8.8.8/"}); close(returned) }()
	select {
	case <-p.entered:
	case <-time.After(time.Second):
		t.Fatal("provider did not enter")
	}
	cancel()
	<-returned
	select {
	case <-p.cleaned:
		t.Fatal("caller cancellation poisoned shared work")
	default:
	}
	closed := make(chan struct{})
	go func() { svc.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close did not cancel detached extraction")
	}
	select {
	case <-p.cleaned:
	default:
		t.Fatal("Close returned before cleanup")
	}
	svc.Close()
}
