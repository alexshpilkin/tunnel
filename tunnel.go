package main

import (
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/http/httputil"

	"golang.org/x/crypto/ssh"
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

type tcpipForward struct {
	Addr string
	Port uint32
}

type tcpipForwardSuccess struct {
	Port uint32
}

type cancelTCPIPForward struct {
	Addr string
	Port uint32
}

func main() {
	handler := &handler{Hosts: map[string]string{
		"localhost:8080": "example.com",
	}}

	config := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			return nil, nil
		},
	}
	privateBytes, err := ioutil.ReadFile("./ssh_host_key")
	if err != nil {
		log.Fatal(err)
	}
	private, err := ssh.ParsePrivateKey(privateBytes)
	if err != nil {
		log.Fatal(err)
	}
	config.AddHostKey(private)

	go func() {
		log.Fatal(http.ListenAndServe(":8080", handler))
	}()

	ln, err := net.Listen("tcp", ":2222")
	if err != nil {
		log.Fatal(err)
	}
	for {
		rw, err := ln.Accept()
		if err != nil {
			log.Print(err)
			continue
		}
		go func() {
			_, chans, reqs, err := ssh.NewServerConn(rw, config)
			if err != nil {
				log.Print(err)
				return
			}

			for reqs != nil && chans != nil {
				select {
				case req, ok := <-reqs:
					if !ok {
						reqs = nil
						continue
					}
					if req.Type == "tcpip-forward" {
						var payload tcpipForward
						if err := ssh.Unmarshal(req.Payload, &payload); err != nil {
							req.Reply(false, nil)
							continue
						}
						log.Printf("Forward: %v %v", payload.Addr, payload.Port)
						if payload.Port != 0 && payload.Port != 80 {
							req.Reply(false, nil)
							continue
						}
						replyPayload := tcpipForwardSuccess{Port: 80}
						req.Reply(true, ssh.Marshal(&replyPayload))
					} else if req.Type == "cancel-tcpip-forward" {
						var payload cancelTCPIPForward
						if err := ssh.Unmarshal(req.Payload, &payload); err != nil {
							req.Reply(false, nil)
							continue
						}
						log.Printf("Cancel forward: %v %v", payload.Addr, payload.Port)
						req.Reply(true, nil)
					}
					req.Reply(false, nil)
				case ch, ok := <-chans:
					if !ok {
						chans = nil
						continue
					}
					ch.Reject(ssh.UnknownChannelType, "No incoming channels accepted")
				}
			}
		}()
	}
}
