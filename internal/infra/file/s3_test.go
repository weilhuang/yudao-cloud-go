package file

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestS3UploadHitsEndpointAndReturnsPublicDomain(t *testing.T) {
	var method, path, body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		path = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client, err := newS3(ClientConfig{
		Endpoint: server.URL, Bucket: "demo", AccessKey: "ak", AccessSecret: "sk",
		Domain: "http://cdn.example.com", EnablePathStyleAccess: true, EnablePublicAccess: boolPtr(true),
	}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	url, err := client.Upload(context.Background(), []byte("hello"), "20260922/a.png", "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPut || !strings.Contains(path, "/demo/20260922/a.png") || body != "hello" {
		t.Fatalf("method %s path %s body %q", method, path, body)
	}
	if url != "http://cdn.example.com/20260922/a.png" {
		t.Fatal(url)
	}
}

func TestLocalRoundTrip(t *testing.T) {
	dir := t.TempDir()
	client := &localClient{id: 29, basePath: dir, domain: "http://127.0.0.1:48080"}
	url, err := client.Upload(context.Background(), []byte("abc"), "20260922/a.txt", "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(url, "/admin-api/infra/file/29/get/20260922/a.txt") {
		t.Fatal(url)
	}
	got, err := client.Get(context.Background(), "20260922/a.txt")
	if err != nil || string(got) != "abc" {
		t.Fatal(string(got), err)
	}
	if _, err := client.resolve("../secret"); err == nil {
		t.Fatal("traversal should fail")
	}
}
