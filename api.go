package apidemic

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/pmylund/go-cache"
)

// Version is the version of apidemic. Apidemic uses semver.
const Version = "0.4"

var maxItemTime = cache.DefaultExpiration

var store = func() *cache.Cache {
	c := cache.New(5*time.Minute, 30*time.Second)
	return c
}()

var events = func() *cache.Cache {
	c := cache.New(5*time.Minute, 30*time.Second)
	return c
}()

var allowedHttpMethods = []string{"OPTIONS", "GET", "POST", "PUT", "DELETE", "HEAD"}

// API is the struct for the json object that is passed to apidemic for registration.
type API struct {
	Endpoint                  string                 `json:"endpoint"`
	HTTPMethod                string                 `json:"http_method"`
	ResponseCodeProbabilities map[int]int            `json:"response_code_probabilities"`
	Payload                   Payload                `json:"payload"`
}

type Payload struct {
	Any map[string]interface{} `json:"any,omitempty"`
	Exactly []map[string]interface{} `json:"exactly,omitempty"`
}

// Home renders hopme page. It renders a json response with information about the service.
func Home(w http.ResponseWriter, r *http.Request) {
	details := make(map[string]interface{})
	details["app_name"] = "ApiDemic"
	details["version"] = Version
	details["details"] = "Fake JSON API response"
	RenderJSON(w, http.StatusOK, details)
	return
}

// FindResponseCode helps imitating the backend responding with an error message occasionally
// Example:
//   {"404": 8, "403": 12, "500": 20, "503": 3}
//   8% chance of getting 404
//   12% chance of getting a 500 error
//   3% chance of getting a 503 error
//   77% chance of getting 200 OK or 201 Created depending on the HTTP method
func FindResponseCode(responseCodeProbabilities map[int]int, method string) int {
	sum := 0
	r := rand.Intn(100)

	for code, probability := range responseCodeProbabilities {
		if probability+sum > r {
			return code
		}
		sum = sum + probability
	}

	if method == "POST" {
		return http.StatusCreated
	}

	return http.StatusOK
}

// RenderJSON helper for rendering JSON response, it marshals value into json and writes
// it into w.
func RenderJSON(w http.ResponseWriter, code int, value interface{}) {
	if code >= 400 || code == http.StatusNoContent {
		http.Error(w, "", code)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	err := json.NewEncoder(w).Encode(value)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// RegisterEndpoint receives API objects and registers them. The payload from the request is
// transformed into a self aware Value that is capable of faking its own attribute.
func RegisterEndpoint(w http.ResponseWriter, r *http.Request) {
	var httpMethod string
	a := API{}
	err := json.NewDecoder(r.Body).Decode(&a)
	if err != nil {
		RenderJSON(w, http.StatusBadRequest, NewResponse(err.Error()))
		return
	}

	if httpMethod, err = getAllowedMethod(a.HTTPMethod); err != nil {
		RenderJSON(w, http.StatusBadRequest, NewResponse(err.Error()))
		return
	}

	eKey, rcpKey := getCacheKeys(a.Endpoint, httpMethod)
	store.Set(eKey, a.Payload, maxItemTime)
	store.Set(rcpKey, a.ResponseCodeProbabilities, maxItemTime)
	RenderJSON(w, http.StatusOK, NewResponse("cool"))
}

func getCacheKeys(endpoint, httpMethod string) (string, string) {
	eKey := fmt.Sprintf("%s-%v-e", endpoint, httpMethod)
	rcpKey := fmt.Sprintf("%s-%v-rcp", endpoint, httpMethod)

	return eKey, rcpKey
}

func getAllowedMethod(method string) (string, error) {
	if method == "" {
		return "GET", nil
	}

	for _, m := range allowedHttpMethods {
		if method == m {
			return m, nil
		}
	}

	return "", errors.New("HTTP method is not allowed")
}

func RouteEndpoint(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	switch vars["endpoint"] {
	case "":
		Home(w, r)
	case "register":
		RegisterEndpoint(w, r)
	case "history":
		HistoryEndpoint(w, r)
	default:
		DynamicEndpoint(w, r)
	}
}

// DynamicEndpoint renders registered endpoints.
func DynamicEndpoint(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		RenderJSON(w, http.StatusInternalServerError, NewResponse(err.Error()))
		return
	}

	events.Set(strconv.Itoa(int(time.Now().UnixNano())), map[string]interface{}{
		"endpoint": path,
		"body": string(body),
		"headers": r.Header,
	}, time.Second * 10)

	eKey, rcpKey := getCacheKeys(path, r.Method)
	if eVal, ok := store.Get(eKey); ok {
		if payload, ok := eVal.(Payload); ok {
			if payload.Any != nil {
				if rcpVal, ok := store.Get(rcpKey); ok {
					code := FindResponseCode(rcpVal.(map[int]int), r.Method)
					RenderJSON(w, code, payload.Any)

					return
				}
			} else if payload.Exactly != nil && len(payload.Exactly) > 0 {
				var pld map[string]interface{}
				pld, payload.Exactly = payload.Exactly[0], payload.Exactly[1:]
				store.Set(eKey, payload, maxItemTime)

				if rcpVal, ok := store.Get(rcpKey); ok {
					code := FindResponseCode(rcpVal.(map[int]int), r.Method)
					RenderJSON(w, code, pld)

					return
				}
			}
		}
	}

	responseText := fmt.Sprintf("apidemic: %s has no %s endpoint", path, r.Method)
	RenderJSON(w, http.StatusNotFound, NewResponse(responseText))
}

func HistoryEndpoint(w http.ResponseWriter, r *http.Request) {
	result := make([]interface{}, 0)

	for _, item := range events.Items() {
		result = append(result, item.Object)
	}

	RenderJSON(w, 200, result)
}

func ResetEndpoint(w http.ResponseWriter, r *http.Request) {
	events.Flush()
	store.Flush()

	RenderJSON(w, 200, nil)
}

// NewResponse helper for response JSON message
func NewResponse(message string) interface{} {
	return struct {
		Text string `json:"text"`
	}{
		message,
	}
}

// NewServer returns a new apidemic server
func NewServer() http.Handler {
	handler := &RegexpHandler{}

	reg, _ := regexp.Compile("^/_register$")
	handler.HandleFunc(reg, RegisterEndpoint)

	reg, _ = regexp.Compile("^/_$")
	handler.HandleFunc(reg, Home)

	reg, _ = regexp.Compile("^/_history$")
	handler.HandleFunc(reg, HistoryEndpoint)

	reg, _ = regexp.Compile("^/_reset$")
	handler.HandleFunc(reg, ResetEndpoint)


	reg, _ = regexp.Compile("^.+")
	handler.HandleFunc(reg, DynamicEndpoint)

	return handler
}

type RegexpHandler struct {
	routes []*route
}

func (h *RegexpHandler) Handler(pattern *regexp.Regexp, handler http.Handler) {
	h.routes = append(h.routes, &route{pattern, handler})
}

func (h *RegexpHandler) HandleFunc(pattern *regexp.Regexp, handler func(http.ResponseWriter, *http.Request)) {
	h.routes = append(h.routes, &route{pattern, http.HandlerFunc(handler)})
}

type route struct {
	pattern *regexp.Regexp
	handler http.Handler
}

func (h *RegexpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	for _, route := range h.routes {
		if route.pattern.MatchString(r.URL.Path) {
			route.handler.ServeHTTP(w, r)
			return
		}
	}
	// no pattern matched; send 404 response
	http.NotFound(w, r)
}
