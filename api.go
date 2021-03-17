package apidemic

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"sort"

	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"

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

var mutex = &sync.Mutex{}

// API is the struct for the json object that is passed to apidemic for registration.
type API struct {
	Endpoint   string     `json:"endpoint"`
	HTTPMethod string     `json:"http_method"`
	Any        *Response  `json:"any,omitempty"`
	Exactly    []Response `json:"exactly,omitempty"`
}

type Response struct {
	Code    int                    `json:"code"`
	Payload map[string]interface{} `json:"payload"`
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

func code(code int) int {
	if code <= 0 {
		return http.StatusOK
	}

	return code
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
		log.Print(err)

		RenderJSON(w, http.StatusBadRequest, NewResponse(err.Error()))
		return
	}

	if httpMethod, err = getAllowedMethod(a.HTTPMethod); err != nil {
		log.Print(err)

		RenderJSON(w, http.StatusBadRequest, NewResponse(err.Error()))
		return
	}

	eKey := getCacheKeys(a.Endpoint, httpMethod)
	store.Set(eKey, a, maxItemTime)
	RenderJSON(w, http.StatusOK, NewResponse("cool"))
}

func getCacheKeys(endpoint, httpMethod string) string {
	return fmt.Sprintf("%s-%v-e", endpoint, httpMethod)
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

// DynamicEndpoint renders registered endpoints.
func DynamicEndpoint(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		events.Set(strconv.Itoa(int(time.Now().UnixNano())), map[string]interface{}{
			"endpoint":        path,
			"body":            "",
			"headers":         r.Header,
			"response_status": http.StatusInternalServerError,
			"response_body":   nil,
		}, time.Second*10)

		RenderJSON(w, http.StatusInternalServerError, NewResponse(err.Error()))
		return
	}

	eKey := getCacheKeys(path, r.Method)
	if eVal, ok := store.Get(eKey); ok {
		api := eVal.(API)
		if api.Any != nil {
			events.Set(strconv.Itoa(int(time.Now().UnixNano())), map[string]interface{}{
				"endpoint":        path,
				"body":            string(body),
				"headers":         r.Header,
				"response_status": code(api.Any.Code),
				"response_body":   api.Any.Payload,
			}, time.Second*10)

			RenderJSON(w, code(api.Any.Code), api.Any.Payload)

			return
		} else if api.Exactly != nil {
			mutex.Lock()
			defer mutex.Unlock()
			if len(api.Exactly) > 0 {
				var apirsp Response
				apirsp, api.Exactly = api.Exactly[0], api.Exactly[1:]
				store.Set(eKey, api, maxItemTime)

				events.Set(strconv.Itoa(int(time.Now().UnixNano())), map[string]interface{}{
					"endpoint":        path,
					"body":            string(body),
					"headers":         r.Header,
					"response_status": code(apirsp.Code),
					"response_body":   apirsp.Payload,
				}, time.Second*10)

				RenderJSON(w, code(apirsp.Code), apirsp.Payload)

				return
			}
		}
	}

	events.Set(strconv.Itoa(int(time.Now().UnixNano())), map[string]interface{}{
		"endpoint":        path,
		"body":            string(body),
		"headers":         r.Header,
		"response_status": http.StatusNotFound,
		"response_body":   nil,
	}, time.Second*10)

	responseText := fmt.Sprintf("apidemic: %s has no %s endpoint", path, r.Method)
	RenderJSON(w, http.StatusNotFound, NewResponse(responseText))
}

func HistoryEndpoint(w http.ResponseWriter, r *http.Request) {
	result := make([]cache.Item, 0)
	for _, item := range events.Items() {
		result = append(result, item)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Expiration < result[j].Expiration
	})

	out := make([]interface{}, 0)
	for i := range result {
		out = append(out, result[i].Object)
	}

	RenderJSON(w, 200, out)
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
