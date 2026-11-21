package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
)

// PathAPIOpenAPI serves the OpenAPI document (SPEC 23.10).
const PathAPIOpenAPI = "/api/v1/openapi.json"

// apiOpenAPI serves each scope's OpenAPI 3.1 document, built once here from
// the same tool lists GET /api/v1/tools answers from, so the two cannot
// drift. The tools' JSON Schemas are copied in unchanged: OpenAPI 3.1 uses
// JSON Schema 2020-12 as is.
func apiOpenAPI(tools map[string][]apiTool) http.Handler {
	docs := map[string][]byte{}
	for scope, list := range tools {
		doc, err := json.Marshal(openAPIDocument(list))
		if err != nil {
			panic(fmt.Sprintf("describe the %s tools: %v", scope, err))
		}
		docs[scope] = doc
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scope := mcpRequestToken(mcpauth.TokenInfoFromContext(r.Context())).Scope
		writeJSON(w, http.StatusOK, json.RawMessage(docs[scope]))
	})
}

func openAPIDocument(tools []apiTool) map[string]any {
	paths := map[string]any{
		PathAPITools: map[string]any{"get": openAPIListOperation()},
	}
	for _, t := range tools {
		paths[PathAPITools+"/"+t.Name] = map[string]any{"post": openAPIToolOperation(t)}
	}
	return map[string]any{
		"openapi": "3.1.0",
		"info":    map[string]any{"title": "Foreman REST API", "version": "1"},
		"components": map[string]any{"securitySchemes": map[string]any{
			"bearerAuth": map[string]any{"type": "http", "scheme": "bearer"},
		}},
		"security": []any{map[string]any{"bearerAuth": []any{}}},
		"paths":    paths,
	}
}

func openAPIListOperation() map[string]any {
	listed := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":          map[string]any{"type": "string"},
			"description":   map[string]any{"type": "string"},
			"read_only":     map[string]any{"type": "boolean"},
			"two_step":      map[string]any{"type": "boolean"},
			"input_schema":  map[string]any{"type": "object"},
			"output_schema": map[string]any{"type": "object"},
		},
		"required": []any{"name", "description", "read_only", "two_step", "input_schema"},
	}
	return map[string]any{
		"operationId": "listTools",
		"summary":     "List the tools this token may call",
		"responses": map[string]any{
			"200": map[string]any{
				"description": "The tools of the token's scope.",
				"content": openAPIJSON(map[string]any{
					"type":       "object",
					"properties": map[string]any{"tools": map[string]any{"type": "array", "items": listed}},
					"required":   []any{"tools"},
				}),
			},
			"401": openAPIPlain("The token is missing, wrong, expired or revoked."),
			"403": openAPIPlain("REST API access is off, or the request came from a web page."),
			"405": openAPIPlain("The method is not GET."),
			"429": openAPIPlain("Too many requests for this token or address."),
			"500": openAPIError("Foreman failed in a way it cannot word. The reason is in Foreman's log.", nil),
		},
	}
}

func openAPIToolOperation(t apiTool) map[string]any {
	// A tool with no output schema may return anything: the empty schema.
	var result any = map[string]any{}
	if t.OutputSchema != nil {
		result = t.OutputSchema
	}
	summary, _, _ := strings.Cut(t.Description, "\n")
	return map[string]any{
		"operationId":         t.Name,
		"summary":             summary,
		"description":         t.Description,
		"x-foreman-read-only": t.ReadOnly,
		"x-foreman-two-step":  t.TwoStep,
		"requestBody": map[string]any{
			"required": false,
			"content":  openAPIJSON(t.InputSchema),
		},
		"responses": map[string]any{
			"200": map[string]any{
				"description": "The tool ran. message is its one-line summary.",
				"content": openAPIJSON(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"message": map[string]any{"type": "string"},
						"result":  result,
					},
					"required": []any{"message", "result"},
				}),
			},
			"400": openAPIError("The body is not one JSON object, or could not be read (then plain text).", nil),
			"401": openAPIPlain("The token is missing, wrong, expired or revoked."),
			"403": openAPIPlain("REST API access is off, or the request came from a web page."),
			"404": openAPIError("No tool of this name for this token.", nil),
			"405": openAPIPlain("The method is not POST."),
			"413": openAPIPlain("The body is over 4 MiB."),
			"415": openAPIError("The body is not application/json.", nil),
			// A refusal's result, such as a preview, need not fit the output schema.
			"422": openAPIError("The tool refused the call, or the arguments do not fit its schema. result holds what the tool gave, such as a fresh preview and confirm code.", map[string]any{}),
			"429": openAPIPlain("Too many requests for this token or address."),
			"500": openAPIError("Foreman failed in a way it cannot word. The reason is in Foreman's log.", nil),
		},
	}
}

func openAPIJSON(schema any) map[string]any {
	return map[string]any{"application/json": map[string]any{"schema": schema}}
}

// openAPIError describes an answer with an English error and, when result
// is not nil, the tool's own result.
func openAPIError(description string, result any) map[string]any {
	props := map[string]any{"error": map[string]any{"type": "string"}}
	if result != nil {
		props["result"] = result
	}
	return map[string]any{
		"description": description,
		"content": openAPIJSON(map[string]any{
			"type": "object", "properties": props, "required": []any{"error"},
		}),
	}
}

// openAPIPlain describes a refusal made before a tool runs, which is plain
// text (SPEC 23.5).
func openAPIPlain(description string) map[string]any {
	return map[string]any{
		"description": description,
		"content":     map[string]any{"text/plain": map[string]any{"schema": map[string]any{"type": "string"}}},
	}
}
