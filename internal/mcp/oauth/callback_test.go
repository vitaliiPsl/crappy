package oauth

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

func TestCallbackFetchesAuthorizationCode(t *testing.T) {
	t.Parallel()

	callback, err := NewCallback(func(url string) { visit(t, url) })
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = callback.Close() }()

	result, err := callback.Fetch(timeout(t), &auth.AuthorizationArgs{URL: callback.URL() + "?code=code&state=state&iss=issuer"})
	if err != nil || result.Code != "code" || result.State != "state" || result.Iss != "issuer" {
		t.Fatalf("authorization result = %+v, %v", result, err)
	}
}

func TestCallbackReportsDeniedAuthorization(t *testing.T) {
	t.Parallel()

	callback, err := NewCallback(func(url string) { visit(t, url) })
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = callback.Close() }()

	args := &auth.AuthorizationArgs{URL: callback.URL() + "?error=access_denied&error_description=User+declined"}
	if _, err := callback.Fetch(timeout(t), args); err == nil || !strings.Contains(err.Error(), "User declined") {
		t.Fatalf("denied authorization = %v", err)
	}
}

func visit(t *testing.T, address string) {
	t.Helper()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, address, nil)
	if err != nil {
		t.Error(err)

		return
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Error(err)

		return
	}

	_ = response.Body.Close()
}

func timeout(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)

	return ctx
}
