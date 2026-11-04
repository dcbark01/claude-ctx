package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

const liteLLMInfo = `{"data":[
 {"model_name":"m1","model_info":{"max_input_tokens":128000,"max_output_tokens":8192}},
 {"model_name":"m2","model_info":{"max_tokens":200000}}]}`

func TestModelInfoLiteLLM(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/model/info" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("x-api-key") != "k" || r.Header.Get("Authorization") != "Bearer k" {
			w.WriteHeader(401)
			return
		}
		w.Write([]byte(liteLLMInfo))
	}))
	defer srv.Close()
	got, err := ModelInfo(context.Background(), srv.URL+"/", "k")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]Limits{"m1": {128000, 8192}, "m2": {200000, 0}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestModelInfoFallbackPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/model/info" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(liteLLMInfo))
	}))
	defer srv.Close()
	got, err := ModelInfo(context.Background(), srv.URL, "k")
	if err != nil || got["m1"].Context != 128000 {
		t.Fatalf("got %v err %v", got, err)
	}
}

func TestModelInfoUnsupported(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	_, err := ModelInfo(context.Background(), srv.URL, "k")
	if !errors.Is(err, ErrNoModelInfo) {
		t.Fatalf("err = %v", err)
	}
}
