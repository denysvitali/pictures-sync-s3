# HTTP Handler Patterns - Quick Reference

This guide shows the recommended patterns for writing HTTP handlers in the pictures-sync-s3 project.

## Basic Handler Pattern

```go
import (
    "net/http"
    "github.com/denysvitali/pictures-sync-s3/pkg/httputil"
    "github.com/denysvitali/pictures-sync-s3/pkg/middleware"
)

func (ctx *Context) HandleExample() http.HandlerFunc {
    // Define request struct (optional)
    type ExampleRequest struct {
        Field1 string `json:"field1"`
        Field2 int    `json:"field2"`
    }

    // Handler logic
    handler := func(w http.ResponseWriter, r *http.Request) error {
        // Your logic here
        httputil.Success(w, map[string]interface{}{
            "message": "success",
        })
        return nil
    }

    // Apply middleware
    wrapped := middleware.Chain(
        middleware.Recovery,
        middleware.RequestLogger,
        middleware.MethodOnly(http.MethodGet),
    )(handler)

    return middleware.Adapt(wrapped)
}
```

## Common Patterns

### 1. GET Handler with Query Parameters

```go
func (ctx *Context) HandleList() http.HandlerFunc {
    handler := func(w http.ResponseWriter, r *http.Request) error {
        // Optional query parameters with defaults
        page := httputil.QueryParamIntRange(r, "page", 1, 1, 1000)
        limit := httputil.QueryParamIntRange(r, "limit", 100, 1, 1000)
        sortBy := httputil.QueryParamDefault(r, "sort", "date")

        // Required query parameter
        id, err := httputil.QueryParam(r, "id")
        if err != nil {
            httputil.BadRequest(w, err.Error())
            return nil
        }

        // Business logic...

        httputil.Success(w, map[string]interface{}{
            "items": []string{"item1", "item2"},
            "total": 2,
        })
        return nil
    }

    wrapped := middleware.Chain(
        middleware.Recovery,
        middleware.MethodOnly(http.MethodGet),
    )(handler)

    return middleware.Adapt(wrapped)
}
```

### 2. POST Handler with JSON Body

```go
func (ctx *Context) HandleCreate() http.HandlerFunc {
    type CreateRequest struct {
        Name  string `json:"name"`
        Value int    `json:"value"`
    }

    handler := func(w http.ResponseWriter, r *http.Request) error {
        var req CreateRequest
        if err := httputil.DecodeJSON(r, &req); err != nil {
            httputil.BadRequest(w, "Invalid JSON: "+err.Error())
            return nil
        }

        // Validate required fields
        if req.Name == "" {
            httputil.BadRequest(w, "name is required")
            return nil
        }

        // Business logic...
        if err := ctx.Service.Create(req.Name, req.Value); err != nil {
            return err // Middleware will handle internal error
        }

        httputil.SuccessWithMessage(w, "Created successfully", map[string]interface{}{
            "id": "123",
        })
        return nil
    }

    wrapped := middleware.Chain(
        middleware.Recovery,
        middleware.MethodOnly(http.MethodPost),
    )(handler)

    return middleware.Adapt(wrapped)
}
```

### 3. Handler with Service Availability Check

```go
func (ctx *Context) HandleWiFi() http.HandlerFunc {
    handler := func(w http.ResponseWriter, r *http.Request) error {
        // Check service availability
        if ctx.WiFiMgr == nil {
            httputil.ServiceUnavailable(w, "WiFi manager not initialized")
            return nil
        }

        // Business logic...
        networks, err := ctx.WiFiMgr.ScanNetworks()
        if err != nil {
            return err // Let middleware handle
        }

        httputil.Success(w, map[string]interface{}{
            "networks": networks,
        })
        return nil
    }

    wrapped := middleware.Chain(
        middleware.Recovery,
        middleware.MethodOnly(http.MethodPost),
    )(handler)

    return middleware.Adapt(wrapped)
}
```

### 4. Handler Supporting Multiple Methods

```go
func (ctx *Context) HandleSettings() http.HandlerFunc {
    handler := func(w http.ResponseWriter, r *http.Request) error {
        switch r.Method {
        case http.MethodGet:
            settings := ctx.AppSettings.ToJSON()
            httputil.JSON(w, http.StatusOK, settings)
            return nil

        case http.MethodPost:
            var req SettingsRequest
            if err := httputil.DecodeJSON(r, &req); err != nil {
                httputil.BadRequest(w, "Invalid JSON: "+err.Error())
                return nil
            }

            if err := ctx.AppSettings.Update(req); err != nil {
                return err
            }

            httputil.SuccessWithMessage(w, "Settings updated", nil)
            return nil

        default:
            httputil.MethodNotAllowed(w)
            return nil
        }
    }

    wrapped := middleware.Chain(
        middleware.Recovery,
        middleware.MethodOnly(http.MethodGet, http.MethodPost),
    )(handler)

    return middleware.Adapt(wrapped)
}
```

## Error Handling

### Standard Error Responses

```go
// 400 Bad Request
httputil.BadRequest(w, "Invalid input")

// 401 Unauthorized
httputil.Unauthorized(w, "Authentication required")

// 403 Forbidden
httputil.Forbidden(w, "Access denied")

// 404 Not Found
httputil.NotFound(w, "Resource not found")

// 405 Method Not Allowed
httputil.MethodNotAllowed(w)

// 409 Conflict
httputil.Conflict(w, "Resource already exists")

// 500 Internal Server Error
httputil.InternalError(w, err)

// Custom error with status code
httputil.Error(w, http.StatusTeapot, "I'm a teapot")
```

### Error Response with Details

```go
httputil.ErrorWithDetails(w, http.StatusBadRequest, "Validation failed", map[string]interface{}{
    "field": "email",
    "reason": "invalid format",
})
```

### Returning Errors for Middleware Handling

```go
// When you want middleware to handle the error (logs + 500 response)
if err := ctx.Service.DoSomething(); err != nil {
    return err // Middleware logs and returns 500
}

// When you want to handle the error yourself (custom response)
if err := ctx.Service.DoSomething(); err != nil {
    httputil.Error(w, http.StatusBadRequest, err.Error())
    return nil // Already handled, don't let middleware process
}
```

## Response Formats

### Success Response

```go
httputil.Success(w, map[string]interface{}{
    "users": []User{user1, user2},
    "total": 2,
})

// Output:
// {
//   "success": true,
//   "data": {
//     "users": [...],
//     "total": 2
//   }
// }
```

### Success with Message

```go
httputil.SuccessWithMessage(w, "User created", map[string]interface{}{
    "id": "123",
})

// Output:
// {
//   "success": true,
//   "message": "User created",
//   "data": {
//     "id": "123"
//   }
// }
```

### Custom JSON Response

```go
httputil.JSON(w, http.StatusOK, customStruct)
```

## Middleware Options

### Available Middleware

```go
middleware.Recovery          // Panic recovery with stack trace
middleware.RequestLogger     // Log all requests
middleware.MethodOnly(...)   // Restrict to specific methods
middleware.RequireQueryParam("param")  // Validate query params
```

### Combining Middleware

```go
wrapped := middleware.Chain(
    middleware.Recovery,          // First: catch panics
    middleware.RequestLogger,     // Second: log request
    middleware.MethodOnly(http.MethodPost),  // Third: validate method
    middleware.RequireQueryParam("id"),      // Fourth: validate params
)(handler)
```

Order matters! Middleware executes from first to last on the way in, and last to first on the way out.

## Query Parameter Helpers

```go
// Required parameter (returns error if missing)
value, err := httputil.QueryParam(r, "id")

// Optional parameter with default
sort := httputil.QueryParamDefault(r, "sort", "date")

// Integer parameter with default
page := httputil.QueryParamInt(r, "page", 1)

// Integer parameter with range validation (min/max)
limit := httputil.QueryParamIntRange(r, "limit", 100, 1, 1000)
// Returns: default 100, clamped to 1-1000
```

## JSON Decoding

```go
// Standard decode (10MB limit)
var req RequestStruct
if err := httputil.DecodeJSON(r, &req); err != nil {
    httputil.BadRequest(w, "Invalid JSON: "+err.Error())
    return nil
}

// Custom size limit (e.g., 1MB for config uploads)
if err := httputil.DecodeJSONWithLimit(r, &req, 1024*1024); err != nil {
    httputil.BadRequest(w, "Invalid JSON: "+err.Error())
    return nil
}
```

## Method Validation

### Using Middleware (Recommended)

```go
middleware.MethodOnly(http.MethodGet, http.MethodPost)
```

### Using Guard Function (For Switch Cases)

```go
if !httputil.MethodGuard(w, r, http.MethodPost) {
    return nil // Guard already sent 405 response
}
```

## Complete Example

Here's a complete example showing all patterns:

```go
func (ctx *Context) HandleUsers() http.HandlerFunc {
    type CreateUserRequest struct {
        Username string `json:"username"`
        Email    string `json:"email"`
    }

    handler := func(w http.ResponseWriter, r *http.Request) error {
        switch r.Method {
        case http.MethodGet:
            // List users with pagination
            page := httputil.QueryParamIntRange(r, "page", 1, 1, 1000)
            limit := httputil.QueryParamIntRange(r, "limit", 20, 1, 100)

            users, total, err := ctx.UserService.List(page, limit)
            if err != nil {
                return err // Let middleware handle
            }

            httputil.Success(w, map[string]interface{}{
                "users": users,
                "total": total,
                "page":  page,
                "limit": limit,
            })
            return nil

        case http.MethodPost:
            // Create user
            var req CreateUserRequest
            if err := httputil.DecodeJSON(r, &req); err != nil {
                httputil.BadRequest(w, "Invalid JSON: "+err.Error())
                return nil
            }

            // Validate
            if req.Username == "" || req.Email == "" {
                httputil.BadRequest(w, "username and email are required")
                return nil
            }

            // Business logic
            user, err := ctx.UserService.Create(req.Username, req.Email)
            if err != nil {
                httputil.Error(w, http.StatusConflict, err.Error())
                return nil
            }

            httputil.SuccessWithMessage(w, "User created", map[string]interface{}{
                "id":       user.ID,
                "username": user.Username,
            })
            return nil

        default:
            httputil.MethodNotAllowed(w)
            return nil
        }
    }

    wrapped := middleware.Chain(
        middleware.Recovery,
        middleware.RequestLogger,
        middleware.MethodOnly(http.MethodGet, http.MethodPost),
    )(handler)

    return middleware.Adapt(wrapped)
}
```

## Migration from Old Patterns

### Old Pattern

```go
func (ctx *Context) OldHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
        return
    }

    var req RequestStruct
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid JSON", http.StatusBadRequest)
        return
    }

    JSONResponse(w, map[string]interface{}{
        "success": true,
    })
}
```

### New Pattern

```go
func (ctx *Context) NewHandler() http.HandlerFunc {
    type RequestStruct struct {
        Field string `json:"field"`
    }

    handler := func(w http.ResponseWriter, r *http.Request) error {
        var req RequestStruct
        if err := httputil.DecodeJSON(r, &req); err != nil {
            httputil.BadRequest(w, "Invalid JSON: "+err.Error())
            return nil
        }

        httputil.Success(w, nil)
        return nil
    }

    wrapped := middleware.Chain(
        middleware.Recovery,
        middleware.MethodOnly(http.MethodPost),
    )(handler)

    return middleware.Adapt(wrapped)
}
```

## Testing Handlers

```go
func TestHandler(t *testing.T) {
    ctx := &Context{
        // Mock dependencies
    }

    handler := ctx.NewHandler()

    req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"field":"value"}`))
    w := httptest.NewRecorder()

    handler(w, req)

    if w.Code != http.StatusOK {
        t.Errorf("Expected status 200, got %d", w.Code)
    }

    var response httputil.Response
    json.NewDecoder(w.Body).Decode(&response)

    if !response.Success {
        t.Error("Expected success=true")
    }
}
```

## Best Practices

1. **Always use middleware.Recovery** - Prevents panics from crashing the service
2. **Validate early** - Check required fields before business logic
3. **Return explicit errors** - Don't use generic "error" messages
4. **Use typed request structs** - Better than map[string]interface{}
5. **Log appropriately** - Use RequestLogger middleware for automatic logging
6. **Handle nil dependencies** - Always check if services are initialized
7. **Use proper status codes** - 400 for client errors, 500 for server errors
8. **Return nil after handling** - If you send a response, return nil (not the error)
9. **Let middleware handle unhandled errors** - Return actual errors for 500s
10. **Test your handlers** - Write tests for happy path and error cases
