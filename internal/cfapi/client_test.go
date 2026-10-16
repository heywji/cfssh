package cfapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEnsureRemoteTunnelAndIngress(t *testing.T) {
	var gotIngress string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("auth header %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/cfd_tunnel/") && strings.HasSuffix(r.URL.Path, "/token"):
			writeJSON(w, map[string]any{"success": true, "result": "token-from-get"})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/cfd_tunnel"):
			writeJSON(w, map[string]any{"success": true, "result": []any{}})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/cfd_tunnel"):
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"config_src":"cloudflare"`) {
				t.Fatalf("body %s", body)
			}
			writeJSON(w, map[string]any{"success": true, "result": map[string]any{"id": "tun-1", "name": "cfssh"}})
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/configurations"):
			body, _ := io.ReadAll(r.Body)
			gotIngress = string(body)
			writeJSON(w, map[string]any{"success": true, "result": map[string]any{}})
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	c := New("test-token")
	c.Base = srv.URL
	c.HTTP = srv.Client()
	tun, err := c.EnsureRemoteTunnel(context.Background(), "acc", "cfssh")
	if err != nil {
		t.Fatal(err)
	}
	if tun.ID != "tun-1" || tun.Token != "token-from-get" {
		t.Fatalf("%+v", tun)
	}
	if err := c.PutSSHIngress(context.Background(), "acc", tun.ID, "ssh.example.com", "ssh://127.0.0.1:2222"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotIngress, "ssh://127.0.0.1:2222") || !strings.Contains(gotIngress, "http_status:404") {
		t.Fatal(gotIngress)
	}
}

func TestAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"success": false,
			"errors":  []any{map[string]any{"code": 1000, "message": "nope"}},
		})
	}))
	defer srv.Close()
	c := New("t")
	c.Base = srv.URL
	c.HTTP = srv.Client()
	_, err := c.ListAccounts(context.Background())
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatal(err)
	}
}

func TestEnsureAccessPolicyBody(t *testing.T) {
	var policyBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/access/apps") && r.Method == http.MethodGet:
			writeJSON(w, map[string]any{"success": true, "result": []any{}})
		case strings.HasSuffix(r.URL.Path, "/access/apps") && r.Method == http.MethodPost:
			writeJSON(w, map[string]any{"success": true, "result": map[string]any{"id": "app-1", "domain": "ssh.example.com"}})
		case strings.HasSuffix(r.URL.Path, "/service_tokens") && r.Method == http.MethodGet:
			writeJSON(w, map[string]any{"success": true, "result": []any{}})
		case strings.HasSuffix(r.URL.Path, "/service_tokens") && r.Method == http.MethodPost:
			writeJSON(w, map[string]any{"success": true, "result": map[string]any{
				"id": "tok-1", "client_id": "cid.access", "client_secret": "sek",
			}})
		case strings.HasSuffix(r.URL.Path, "/policies") && r.Method == http.MethodGet:
			writeJSON(w, map[string]any{"success": true, "result": []any{}})
		case strings.HasSuffix(r.URL.Path, "/policies") && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			policyBody = string(body)
			writeJSON(w, map[string]any{"success": true, "result": map[string]any{"id": "pol"}})
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	c := New("t")
	c.Base = srv.URL
	c.HTTP = srv.Client()
	cred, err := c.EnsureAccessSSH(context.Background(), "acc", "ssh.example.com", AccessCredential{})
	if err != nil {
		t.Fatal(err)
	}
	if cred.ClientID != "cid.access" || cred.Secret != "sek" {
		t.Fatal(cred)
	}
	if !strings.Contains(policyBody, "tok-1") || !strings.Contains(policyBody, `"decision":"non_identity"`) {
		t.Fatal(policyBody)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	data, _ := json.Marshal(v)
	_, _ = w.Write(data)
}
