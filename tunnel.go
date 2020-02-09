package main

import (
	"io"
	"log"
	"net/http"
	"net/http/httputil"
)

type handler struct {
	Hosts map[string]string
	proxy *httputil.ReverseProxy
}

func (h *handler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if _, ok := h.Hosts[req.Host]; !ok {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `<html>
<head><title>404 Not Found</title></head>
<body>
<center><h1>404 Not Found</h1></center>
<hr><center>tunnel</center>
</body>
</html>`)
		return
	}

	if h.proxy == nil {
		h.proxy = &httputil.ReverseProxy{Director: h.director}
	}
	h.proxy.ServeHTTP(w, req)
}

func (h *handler) director(req *http.Request) {
	host := h.Hosts[req.Host]
	req.URL.Scheme = "https"
	req.URL.Host = host
	req.Host = host
	if _, ok := req.Header["User-Agent"]; !ok {
		req.Header.Set("User-Agent", "")
	}
}

func main() {
	handler := &handler{Hosts: map[string]string{
		"localhost:8080": "example.com",
	}}
	log.Fatal(http.ListenAndServe(":8080", handler))
}
