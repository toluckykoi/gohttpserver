package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestHTTPStaticServerSetsContentSecurityPolicy(t *testing.T) {
	root, err := os.MkdirTemp("", "gohttpserver-csp-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)

	server := NewHTTPStaticServer(root, true)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	server.ServeHTTP(recorder, request)

	if got := recorder.Header().Get("Content-Security-Policy"); got != contentSecurityPolicy {
		t.Fatalf("Content-Security-Policy header = %q, want %q", got, contentSecurityPolicy)
	}
}

