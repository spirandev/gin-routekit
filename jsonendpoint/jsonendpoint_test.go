package jsonendpoint

import (
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	routekit "github.com/spirandev/gin-routekit"
)

type requestDTO struct {
	Name string `json:"name" binding:"required"`
}

type responseDTO struct {
	Message string `json:"message"`
}

type failingResponse struct{}

func (failingResponse) MarshalJSON() ([]byte, error) {
	return nil, errors.New("cannot marshal")
}

type cyclicResponse struct {
	Next *cyclicResponse `json:"next,omitempty"`
}

type recursiveSlice []recursiveSlice

type RequestFields struct {
	Name string `json:"name"`
}

type embeddedRequest struct {
	RequestFields
}

type containerRequest struct {
	Items []string `json:"items"`
}

type containerResponse struct {
	Items []string `json:"items"`
}

type unsupportedNestedResponse struct {
	Value uintptr `json:"value"`
}

type nullabilityRequest struct {
	Count     int        `json:"count"`
	Timestamp *time.Time `json:"timestamp"`
}

type UUID struct {
	Value string `json:"value"`
}

func TestHandleRunsAndDerivesContract(t *testing.T) {
	engine, group := newTestGroup()
	var received requestDTO
	Handle(group, http.MethodPost, "/resources", func(_ *gin.Context, request requestDTO) (responseDTO, error) {
		received = request
		return responseDTO{Message: "hello " + request.Name}, nil
	}, "Create resource", 10, WithSuccess(http.StatusCreated, "Created"))

	route := group.Export("resources", 99)
	recorder := performJSON(engine, http.MethodPost, "/api/resources", `{"name":"Ada"}`)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", recorder.Code)
	}
	if got := strings.TrimSpace(recorder.Body.String()); got != `{"message":"hello Ada"}` {
		t.Fatalf("body = %s", got)
	}
	if received.Name != "Ada" {
		t.Fatalf("request = %#v", received)
	}

	contract := route.Handlers[0].Contract
	if contract == nil || contract.RequestBody == nil || !contract.RequestBody.Required {
		t.Fatalf("derived contract request body = %#v", contract)
	}
	requestSchema := contract.RequestBody.Schema.(routekit.SchemaDescriptor)
	if requestSchema.Type != reflect.TypeOf(requestDTO{}) {
		t.Errorf("request schema type = %v", requestSchema.Type)
	}
	wantStatuses := []int{http.StatusCreated, http.StatusBadRequest, http.StatusInternalServerError}
	for i, want := range wantStatuses {
		if got := contract.Responses[i].Status; got != want {
			t.Errorf("response %d status = %d, want %d", i, got, want)
		}
		if contract.Responses[i].ContentType != "application/json" {
			t.Errorf("response %d content type = %q", i, contract.Responses[i].ContentType)
		}
	}
	responseSchema := contract.Responses[0].Schema.(routekit.SchemaDescriptor)
	if responseSchema.Type != reflect.TypeOf(responseDTO{}) {
		t.Errorf("response schema type = %v", responseSchema.Type)
	}
}

func TestHandleSupportsRequestShapes(t *testing.T) {
	t.Run("pointer", func(t *testing.T) {
		engine, group := newTestGroup()
		var received *requestDTO
		Handle(group, http.MethodPost, "/pointer", func(_ *gin.Context, request *requestDTO) (responseDTO, error) {
			received = request
			return responseDTO{Message: request.Name}, nil
		}, "pointer", 1)
		group.Export("test", 1)

		recorder := performJSON(engine, http.MethodPost, "/api/pointer", `{"name":"pointer"}`)
		if recorder.Code != http.StatusOK || received == nil || received.Name != "pointer" {
			t.Fatalf("status = %d, request = %#v", recorder.Code, received)
		}
	})

	t.Run("optional absent pointer stays nil", func(t *testing.T) {
		engine, group := newTestGroup()
		var received *requestDTO
		Handle(group, http.MethodPost, "/optional", func(_ *gin.Context, request *requestDTO) (responseDTO, error) {
			received = request
			return responseDTO{}, nil
		}, "optional", 1, WithOptionalBody())
		group.Export("test", 1)

		recorder := performRequest(engine, http.MethodPost, "/api/optional", "", "")
		if recorder.Code != http.StatusOK || received != nil {
			t.Fatalf("status = %d, request = %#v", recorder.Code, received)
		}
	})

	t.Run("slice", func(t *testing.T) {
		engine, group := newTestGroup()
		var received []requestDTO
		Handle(group, http.MethodPost, "/slice", func(_ *gin.Context, request []requestDTO) (responseDTO, error) {
			received = request
			return responseDTO{}, nil
		}, "slice", 1)
		group.Export("test", 1)

		recorder := performJSON(engine, http.MethodPost, "/api/slice", `[{"name":"one"}]`)
		if recorder.Code != http.StatusOK || len(received) != 1 || received[0].Name != "one" {
			t.Fatalf("status = %d, request = %#v", recorder.Code, received)
		}
	})

	t.Run("map", func(t *testing.T) {
		engine, group := newTestGroup()
		var received map[string]int
		Handle(group, http.MethodPost, "/map", func(_ *gin.Context, request map[string]int) (responseDTO, error) {
			received = request
			return responseDTO{}, nil
		}, "map", 1)
		group.Export("test", 1)

		recorder := performJSON(engine, http.MethodPost, "/api/map", `{"one":1}`)
		if recorder.Code != http.StatusOK || received["one"] != 1 {
			t.Fatalf("status = %d, request = %#v", recorder.Code, received)
		}
	})
}

func TestHandleRoutesBindingAndValidationErrors(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		contentType string
		options     []Option
	}{
		{name: "absent body"},
		{name: "whitespace body", body: "  \n"},
		{name: "malformed JSON", body: "{", contentType: "application/json"},
		{name: "multiple JSON values", body: `{"name":"Ada"}{"name":"ignored"}`, contentType: "application/json"},
		{name: "missing required field", body: `{}`, contentType: "application/json"},
		{name: "wrong content type", body: `{"name":"Ada"}`, contentType: "text/plain"},
		{name: "body too large", body: `{"name":"Ada"}`, contentType: "application/json", options: []Option{WithBodyLimit(4)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engine, group := newTestGroup()
			called := false
			Handle(group, http.MethodPost, "/resource", func(_ *gin.Context, request requestDTO) (responseDTO, error) {
				called = true
				return responseDTO{}, nil
			}, "resource", 1, test.options...)
			group.Export("test", 1)

			recorder := performRequest(engine, http.MethodPost, "/api/resource", test.body, test.contentType)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", recorder.Code)
			}
			if called {
				t.Fatal("typed handler ran after binding failure")
			}
			if got := strings.TrimSpace(recorder.Body.String()); got != `{"error":"invalid request"}` {
				t.Errorf("body = %s", got)
			}
		})
	}

	t.Run("additional validator", func(t *testing.T) {
		engine, group := newTestGroup()
		Handle(group, http.MethodPost, "/resource", func(_ *gin.Context, request requestDTO) (responseDTO, error) {
			return responseDTO{}, nil
		}, "resource", 1, WithValidator(func(_ *gin.Context, request requestDTO) error {
			if request.Name == "blocked" {
				return errors.New("blocked")
			}
			return nil
		}))
		group.Export("test", 1)

		recorder := performJSON(engine, http.MethodPost, "/api/resource", `{"name":"blocked"}`)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", recorder.Code)
		}
	})

	t.Run("nested null container", func(t *testing.T) {
		engine, group := newTestGroup()
		Handle(group, http.MethodPost, "/resource", func(_ *gin.Context, request containerRequest) (responseDTO, error) {
			return responseDTO{}, nil
		}, "resource", 1)
		group.Export("test", 1)

		recorder := performJSON(engine, http.MethodPost, "/api/resource", `{"items":null}`)
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", recorder.Code)
		}
	})

	t.Run("top-level JSON null", func(t *testing.T) {
		engine, group := newTestGroup()
		called := false
		Handle(group, http.MethodPost, "/resource", func(_ *gin.Context, request containerRequest) (responseDTO, error) {
			called = true
			return responseDTO{}, nil
		}, "resource", 1)
		group.Export("test", 1)
		recorder := performJSON(engine, http.MethodPost, "/api/resource", `null`)
		if recorder.Code != http.StatusOK || !called {
			t.Fatalf("status = %d, called = %v", recorder.Code, called)
		}
	})

	t.Run("non-pointer scalar null", func(t *testing.T) {
		engine, group := newTestGroup()
		Handle(group, http.MethodPost, "/resource", func(_ *gin.Context, request nullabilityRequest) (responseDTO, error) {
			return responseDTO{}, nil
		}, "resource", 1)
		group.Export("test", 1)

		recorder := performJSON(engine, http.MethodPost, "/api/resource", `{"count":null}`)
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", recorder.Code)
		}
	})

	t.Run("time pointer null", func(t *testing.T) {
		engine, group := newTestGroup()
		Handle(group, http.MethodPost, "/resource", func(_ *gin.Context, request nullabilityRequest) (responseDTO, error) {
			return responseDTO{}, nil
		}, "resource", 1)
		group.Export("test", 1)

		recorder := performJSON(engine, http.MethodPost, "/api/resource", `{"timestamp":null}`)
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", recorder.Code)
		}
	})
}

func TestHandleRoutesHandlerAndSerializationErrors(t *testing.T) {
	t.Run("handler error", func(t *testing.T) {
		engine, group := newTestGroup()
		Handle(group, http.MethodPost, "/resource", func(_ *gin.Context, request requestDTO) (responseDTO, error) {
			return responseDTO{}, errors.New("failed")
		}, "resource", 1)
		group.Export("test", 1)

		recorder := performJSON(engine, http.MethodPost, "/api/resource", `{"name":"Ada"}`)
		assertInternalError(t, recorder)
	})

	t.Run("nil slice response", func(t *testing.T) {
		engine, group := newTestGroup()
		Handle(group, http.MethodPost, "/resource", func(_ *gin.Context, request requestDTO) ([]responseDTO, error) {
			return nil, nil
		}, "resource", 1)
		group.Export("test", 1)

		recorder := performJSON(engine, http.MethodPost, "/api/resource", `{"name":"Ada"}`)
		if recorder.Code != http.StatusOK || strings.TrimSpace(recorder.Body.String()) != "null" {
			t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
		}
	})

	t.Run("serialization error", func(t *testing.T) {
		engine, group := newTestGroup()
		Handle(group, http.MethodPost, "/resource", func(_ *gin.Context, request requestDTO) (cyclicResponse, error) {
			response := cyclicResponse{}
			response.Next = &response
			return response, nil
		}, "resource", 1)
		group.Export("test", 1)

		recorder := performJSON(engine, http.MethodPost, "/api/resource", `{"name":"Ada"}`)
		assertInternalError(t, recorder)
	})

	t.Run("nested nil container response", func(t *testing.T) {
		engine, group := newTestGroup()
		Handle(group, http.MethodPost, "/resource", func(_ *gin.Context, request requestDTO) (containerResponse, error) {
			return containerResponse{}, nil
		}, "resource", 1)
		group.Export("test", 1)

		recorder := performJSON(engine, http.MethodPost, "/api/resource", `{"name":"Ada"}`)
		if recorder.Code != http.StatusOK || strings.TrimSpace(recorder.Body.String()) != `{"items":null}` {
			t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
		}
	})
}

func TestHandleDoesNotWriteTwiceAndStrictModeRecordsViolation(t *testing.T) {
	engine, group := newTestGroup()
	var strictViolation bool
	var handlerErrorObserved bool
	group.Use(func(context *gin.Context) {
		context.Next()
		for _, contextError := range context.Errors {
			if errors.Is(contextError.Err, ErrResponseAlreadyWritten) {
				strictViolation = true
			}
			if strings.Contains(contextError.Error(), "handler failed after write") {
				handlerErrorObserved = true
			}
		}
	})
	Handle(group, http.MethodPost, "/resource", func(context *gin.Context, request requestDTO) (responseDTO, error) {
		context.JSON(http.StatusAccepted, gin.H{"written": true})
		return responseDTO{Message: "second"}, errors.New("handler failed after write")
	}, "resource", 1, WithStrictWriter())
	group.Export("test", 1)

	recorder := performJSON(engine, http.MethodPost, "/api/resource", `{"name":"Ada"}`)
	if recorder.Code != http.StatusAccepted || strings.TrimSpace(recorder.Body.String()) != `{"written":true}` {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
	if !strictViolation {
		t.Fatal("strict mode did not record writer violation")
	}
	if !handlerErrorObserved {
		t.Fatal("handler error was lost after writing the response")
	}
}

func TestHandleOwnsResponseContentType(t *testing.T) {
	engine, group := newTestGroup()
	Handle(group, http.MethodPost, "/resource", func(context *gin.Context, request requestDTO) (responseDTO, error) {
		context.Header("Content-Type", "text/plain")
		return responseDTO{Message: "json"}, nil
	}, "resource", 1)
	group.Export("test", 1)

	recorder := performJSON(engine, http.MethodPost, "/api/resource", `{"name":"Ada"}`)
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
}

func TestHandleNoContentMatchesContract(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusResetContent} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			engine, group := newTestGroup()
			Handle(group, http.MethodPost, "/resource", func(_ *gin.Context, request requestDTO) (responseDTO, error) {
				return responseDTO{Message: "ignored"}, nil
			}, "resource", 1, WithSuccess(status, ""))
			route := group.Export("test", 1)

			recorder := performJSON(engine, http.MethodPost, "/api/resource", `{"name":"Ada"}`)
			if recorder.Code != status || recorder.Body.Len() != 0 {
				t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
			}
			response := route.Handlers[0].Contract.Responses[0]
			if response.Description != http.StatusText(status) || response.Schema != nil {
				t.Fatalf("documented response = %#v", response)
			}
		})
	}
}

func TestHandleGeneratedOpenAPIMatchesRuntime(t *testing.T) {
	engine, group := newTestGroup()
	Handle(group, http.MethodPost, "/resource", func(_ *gin.Context, request requestDTO) (responseDTO, error) {
		return responseDTO{}, nil
	}, "resource", 1, WithSuccess(http.StatusCreated, "Created"))
	route := group.Export("test", 1)

	document, err := routekit.BuildOpenAPI([]routekit.Route{route}, routekit.OpenAPIConfig{
		Title:             "Test",
		Version:           "1",
		BasePath:          "/",
		PathMode:          routekit.FullRegisteredPaths,
		DocumentationMode: routekit.DocumentAll,
	})
	if err != nil {
		t.Fatalf("BuildOpenAPI: %v", err)
	}
	operation := document.Paths["/api/resource"].Post
	if operation == nil || operation.RequestBody == nil || !operation.RequestBody.Required {
		t.Fatalf("operation = %#v", operation)
	}
	for _, status := range []string{"201", "400", "500"} {
		response, ok := operation.Responses[status]
		if !ok || response.Content["application/json"].Schema == nil {
			t.Errorf("missing documented JSON response %s: %#v", status, response)
		}
	}

	recorder := performJSON(engine, http.MethodPost, "/api/resource", `{"name":"Ada"}`)
	if recorder.Code != http.StatusCreated || !strings.HasPrefix(recorder.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("runtime response = %d %q", recorder.Code, recorder.Header().Get("Content-Type"))
	}
}

func TestHandlePreservesDocumentationOptIn(t *testing.T) {
	_, group := newTestGroup()
	Handle(group, http.MethodPost, "/resource", func(_ *gin.Context, request requestDTO) (responseDTO, error) {
		return responseDTO{}, nil
	}, "resource", 1)
	route := group.Export("test", 1)

	document, err := routekit.BuildOpenAPI([]routekit.Route{route}, routekit.OpenAPIConfig{Title: "Test", Version: "1", BasePath: "/", PathMode: routekit.FullRegisteredPaths})
	if err != nil {
		t.Fatalf("BuildOpenAPI: %v", err)
	}
	if len(document.Paths) != 0 {
		t.Fatalf("typed endpoint unexpectedly opted into documentation: %#v", document.Paths)
	}
}

func TestHandleRejectsInvalidStaticConfiguration(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*routekit.RouterGroup)
	}{
		{
			name: "HEAD",
			configure: func(group *routekit.RouterGroup) {
				Handle(group, http.MethodHead, "/resource", func(_ *gin.Context, request requestDTO) (responseDTO, error) { return responseDTO{}, nil }, "resource", 1)
			},
		},
		{
			name: "TRACE",
			configure: func(group *routekit.RouterGroup) {
				Handle(group, http.MethodTrace, "/resource", func(_ *gin.Context, request requestDTO) (responseDTO, error) { return responseDTO{}, nil }, "resource", 1)
			},
		},
		{
			name: "invalid body limit",
			configure: func(group *routekit.RouterGroup) {
				Handle(group, http.MethodPost, "/resource", func(_ *gin.Context, request requestDTO) (responseDTO, error) { return responseDTO{}, nil }, "resource", 1, WithBodyLimit(0))
			},
		},
		{
			name: "max body limit overflow",
			configure: func(group *routekit.RouterGroup) {
				Handle(group, http.MethodPost, "/resource", func(_ *gin.Context, request requestDTO) (responseDTO, error) { return responseDTO{}, nil }, "resource", 1, WithBodyLimit(math.MaxInt64))
			},
		},
		{
			name: "validator type mismatch",
			configure: func(group *routekit.RouterGroup) {
				Handle(group, http.MethodPost, "/resource", func(_ *gin.Context, request requestDTO) (responseDTO, error) { return responseDTO{}, nil }, "resource", 1, WithValidator(func(_ *gin.Context, request []requestDTO) error { return nil }))
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, group := newTestGroup()
			defer func() {
				recovered := recover()
				if recovered == nil || !strings.HasPrefix(recovered.(string), "jsonendpoint:") {
					t.Fatalf("panic = %#v", recovered)
				}
			}()
			test.configure(group)
		})
	}
}

func newTestGroup() (*gin.Engine, *routekit.RouterGroup) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	return engine, routekit.NewRouterGroup(engine, "/api")
}

func performJSON(engine *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	return performRequest(engine, method, path, body, "application/json")
}

func performRequest(engine *gin.Engine, method, path, body, contentType string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	engine.ServeHTTP(recorder, request)
	return recorder
}

func assertInternalError(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	if got := strings.TrimSpace(recorder.Body.String()); got != `{"error":"internal server error"}` {
		t.Errorf("body = %s", got)
	}
}
