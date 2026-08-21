package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer k" {
			w.WriteHeader(401)
			w.Write([]byte(`{"error":"bad key"}`))
			return
		}
		w.Write([]byte(`{"object":"list","data":[{"id":"a"},{"id":"b"}],"models":[{"name":"a"}]}`))
	}))
	defer srv.Close()
	ids, err := ListModels(context.Background(), srv.URL+"/", "k")
	if err != nil || strings.Join(ids, ",") != "a,b" {
		t.Fatalf("ids=%v err=%v", ids, err)
	}
	if _, err := ListModels(context.Background(), srv.URL, "wrong"); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v", err)
	}
}
