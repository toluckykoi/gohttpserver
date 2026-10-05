package util

import (
	"errors"
	"net"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"
)

// newTestServer starts an http.Server on a loopback listener, waits until
// it is actually accepting connections, and returns the server plus a
// channel carrying its Serve return value.
func newTestServer(t *testing.T) (*http.Server, chan error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()

	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(2 * time.Second)
	for {
		resp, err := client.Get("http://" + ln.Addr().String() + "/")
		if err == nil {
			resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never became ready: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return srv, done
}

func TestShutdownManagerShutdownClosesServerAndReturnsErrServerClosed(t *testing.T) {
	srv, done := newTestServer(t)

	var cleanupRan bool
	sm := NewShutdownManager(srv, nil, 5*time.Second)
	sm.RegisterCleanup(func() error {
		cleanupRan = true
		return nil
	})

	if err := sm.Shutdown(); err != nil {
		t.Fatalf("Shutdown returned error: %v", err)
	}

	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("Serve returned %v, want http.ErrServerClosed", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return after Shutdown")
	}

	if !cleanupRan {
		t.Fatal("registered cleanup did not run")
	}
}

// Cleanups must run newest-first (LIFO) and a failing cleanup must not
// prevent the remaining ones from running.
func TestShutdownManagerRunsCleanupsLIFOAndToleratesErrors(t *testing.T) {
	srv, done := newTestServer(t)

	var mu sync.Mutex
	var order []string
	record := func(name string) func() error {
		return func() error {
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
			return nil
		}
	}

	sm := NewShutdownManager(srv, nil, 5*time.Second)
	sm.RegisterCleanup(record("first"))
	sm.RegisterCleanup(func() error {
		mu.Lock()
		order = append(order, "second")
		mu.Unlock()
		return errors.New("boom")
	})
	sm.RegisterCleanup(record("third"))

	if err := sm.Shutdown(); err != nil {
		t.Fatalf("Shutdown returned error: %v", err)
	}
	<-done

	want := []string{"third", "second", "first"}
	mu.Lock()
	got := append([]string(nil), order...)
	mu.Unlock()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cleanup order = %v, want %v", got, want)
	}
}
