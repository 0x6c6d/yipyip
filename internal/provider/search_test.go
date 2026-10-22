package provider_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"yipyip/internal/provider"
	"yipyip/internal/provider/providertest"
)

const searchKey = "brave-test-key-55"

func searchClient(t *testing.T, b *providertest.BraveServer) *provider.SearchClient {
	t.Helper()
	hc, err := provider.NewSearchHTTPClient(b.URL)
	if err != nil {
		t.Fatal(err)
	}
	return provider.NewSearchClient(hc, b.URL, searchKey)
}

func TestSearchClientRequest(t *testing.T) {
	b := providertest.NewBrave(func(int, providertest.BraveRequest) providertest.BraveResponse {
		return providertest.BraveResponse{}
	})
	defer b.Close()
	q := "go generics & more?"
	if _, err := searchClient(t, b).Search(context.Background(), q, 3); err != nil {
		t.Fatal(err)
	}
	reqs := b.Requests()
	if len(reqs) != 1 {
		t.Fatalf("requests %d", len(reqs))
	}
	r := reqs[0]
	if r.Method != "GET" || r.Path != "/res/v1/web/search" || r.Query.Get("q") != q || r.Query.Get("count") != "3" {
		t.Fatalf("request %+v", r)
	}
	if r.Header.Get("X-Subscription-Token") != searchKey || r.Header.Get("Accept") != "application/json" {
		t.Fatalf("headers %v", r.Header)
	}
	if _, ok := r.Header["Authorization"]; ok {
		t.Fatalf("Authorization header sent: %v", r.Header)
	}
}

func TestSearchClientResults(t *testing.T) {
	b := providertest.NewBrave(func(n int, _ providertest.BraveRequest) providertest.BraveResponse {
		if n == 1 {
			return providertest.BraveResponse{Body: `{"type":"search"}`}
		}
		return providertest.BraveResponse{Results: []providertest.BraveResult{
			{Title: "A <b>x</b>", URL: "https://a.example/", Description: "d &amp; 1"},
			{Title: "B", URL: "https://b.example/", Description: "d2"},
		}}
	})
	defer b.Close()
	c := searchClient(t, b)
	res, err := c.Search(context.Background(), "q", 5)
	if err != nil || len(res) != 2 {
		t.Fatalf("%+v %v", res, err)
	}
	if res[0] != (provider.SearchResult{Title: "A <b>x</b>", URL: "https://a.example/", Description: "d &amp; 1"}) || res[1].Title != "B" {
		t.Fatalf("%+v", res)
	}
	res, err = c.Search(context.Background(), "q", 5)
	if err != nil || res == nil || len(res) != 0 {
		t.Fatalf("%#v %v", res, err)
	}
}

func TestSearchClientNon200(t *testing.T) {
	b := providertest.NewBrave(func(n int, _ providertest.BraveRequest) providertest.BraveResponse {
		st := 500
		if n == 1 {
			st = 401
		}
		return providertest.BraveResponse{Status: st, Body: searchKey + " upstream detail"}
	})
	defer b.Close()
	c := searchClient(t, b)
	for _, want := range []int{500, 401} {
		_, err := c.Search(context.Background(), "q", 5)
		var se *provider.SearchStatusError
		if !errors.As(err, &se) || se.Code != want {
			t.Fatalf("want %d: %v", want, err)
		}
		if strings.Contains(err.Error(), searchKey) || strings.Contains(err.Error(), "upstream detail") {
			t.Fatalf("error echoes: %v", err)
		}
	}
}

func TestSearchClientBadJSON(t *testing.T) {
	b := providertest.NewBrave(func(int, providertest.BraveRequest) providertest.BraveResponse {
		return providertest.BraveResponse{Body: "{nope"}
	})
	defer b.Close()
	_, err := searchClient(t, b).Search(context.Background(), "q", 5)
	if !errors.Is(err, provider.ErrSearchInvalidResponse) {
		t.Fatalf("%v", err)
	}
}

func TestSearchClientBodyLimit(t *testing.T) {
	body := `{"web":{"results":[{"title":"` + strings.Repeat("a", 2<<20) + `","url":"u","description":"d"}]}}`
	b := providertest.NewBrave(func(int, providertest.BraveRequest) providertest.BraveResponse {
		return providertest.BraveResponse{Body: body}
	})
	defer b.Close()
	_, err := searchClient(t, b).Search(context.Background(), "q", 5)
	if !errors.Is(err, provider.ErrSearchInvalidResponse) {
		t.Fatalf("%v", err)
	}
}

func TestSearchClientCancel(t *testing.T) {
	b := providertest.NewBrave(func(int, providertest.BraveRequest) providertest.BraveResponse {
		return providertest.BraveResponse{Delay: 5 * time.Second}
	})
	defer b.Close()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	start := time.Now()
	_, err := searchClient(t, b).Search(ctx, "q", 5)
	if err == nil || time.Since(start) > 2*time.Second {
		t.Fatalf("err %v after %v", err, time.Since(start))
	}
}
