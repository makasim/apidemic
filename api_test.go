package apidemic

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/pmylund/go-cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDynamicEndpointFailsWithoutRegistration(t *testing.T) {
	s := setUp()
	payload := registerPayload(t, "fixtures/sample_request.json")

	w := httptest.NewRecorder()
	req := jsonRequest("POST", "/api/test", payload)
	s.ServeHTTP(w, req)

	r := mux.NewRouter()
	r.Handle("/api/test", s)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestDynamicEndpointWithGetRequest(t *testing.T) {
	s := setUp()
	payload := registerPayload(t, "fixtures/sample_request.json")

	w := httptest.NewRecorder()
	req := jsonRequest("POST", "/_register", payload)
	s.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	w = httptest.NewRecorder()
	req = jsonRequest("GET", "/test", "")
	s.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestDynamicEndpointWithPostRequest(t *testing.T) {
	s := setUp()
	payload := API{
		Endpoint:   "/api/test",
		HTTPMethod: "POST",
		Any: &Response{
			Code: http.StatusCreated,
			Payload: map[string]interface{}{
				"foo": "val",
			},
		},
	}

	w := httptest.NewRecorder()
	req := jsonRequest("POST", "/_register", payload)
	s.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()

	req = jsonRequest("POST", "/api/test", "")

	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestDynamicEndpointWithForbiddenResponse(t *testing.T) {
	s := setUp()
	payload := API{
		Endpoint:   "/api/test",
		HTTPMethod: "POST",
		Any: &Response{
			Code: http.StatusForbidden,
			Payload: map[string]interface{}{
				"foo": "val",
			},
		},
	}

	w := httptest.NewRecorder()
	req := jsonRequest("POST", "/_register", payload)
	s.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	req = jsonRequest("POST", "/api/test", "")

	s.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func setUp() http.Handler {
	store = cache.New(5*time.Minute, 30*time.Second)

	return NewServer()
}

func registerPayload(t *testing.T, fixtureFile string) map[string]interface{} {
	content, err := ioutil.ReadFile(fixtureFile)
	if err != nil {
		t.Fatal(err)
	}

	var api map[string]interface{}
	err = json.NewDecoder(bytes.NewReader(content)).Decode(&api)
	if err != nil {
		t.Fatal(err)
	}

	return api
}

func jsonRequest(method string, path string, body interface{}) *http.Request {
	var bEnd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil
		}
		bEnd = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, path, bEnd)
	if err != nil {
		panic(err)
	}

	req.Header.Set("Content-Type", "application/json")
	return req
}
