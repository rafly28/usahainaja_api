package main

import (
"fmt"
"net/http"
"net/http/httptest"
)

func main() {
ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
defer ts.Close()

c1 := ts.Client()
c2 := ts.Client()

fmt.Printf("c1: %p, c2: %p\n", c1, c2)
}
