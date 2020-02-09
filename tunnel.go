package main

import (
	"log"
	"net/http"
	"net/http/httputil"
)

func director(req *http.Request) {
	req.URL.Scheme = "https"
	req.URL.Host = "example.com"
	req.Host = "example.com"
	if _, ok := req.Header["User-Agent"]; !ok {
		req.Header.Set("User-Agent", "")
	}
}

func main() {
	proxy := &httputil.ReverseProxy{Director: director}
	log.Fatal(http.ListenAndServe(":8080", proxy))
}
