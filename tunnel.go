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

type Server struct {
	SSHAddr   string
	HTTPAddr  string
	SSHConfig *ssh.ServerConfig
	hosts     map[string]string
	proxy     *httputil.ReverseProxy
}

func (s *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if _, ok := s.hosts[req.Host]; !ok {
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

	if s.proxy == nil {
		s.proxy = &httputil.ReverseProxy{Director: s.director}
	}
	s.proxy.ServeHTTP(w, req)
}

func (s *Server) director(req *http.Request) {
	host := s.hosts[req.Host]
	req.URL.Scheme = "https"
	req.URL.Host = host
	req.Host = host
	if _, ok := req.Header["User-Agent"]; !ok {
		req.Header.Set("User-Agent", "")
	}
}

func (s *Server) serveSSH(l net.Listener) error {
	for {
		rw, err := l.Accept()
		if err != nil {
			log.Print(err)
			continue
		}
		go func() {
			_, chans, reqs, err := ssh.NewServerConn(rw, s.SSHConfig)
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

func (s *Server) serveHTTP(l net.Listener) error {
	return http.Serve(l, s)
}

func (s *Server) Serve(sshL net.Listener, httpL net.Listener) error {
	sshError := make(chan error, 1)
	httpError := make(chan error, 1)

	go func() {
		sshError <- s.serveSSH(sshL)
	}()
	go func() {
		httpError <- s.serveHTTP(httpL)
	}()

	select {
	case e := <-sshError:
		return e
	case e := <-httpError:
		return e
	}
}

func (s *Server) ListenAndServe() error {
	sshAddr := s.SSHAddr
	if sshAddr == "" {
		sshAddr = ":ssh"
	}
	sshL, err := net.Listen("tcp", sshAddr)
	if err != nil {
		return err
	}
	defer sshL.Close() // FIXME multiple close?

	httpAddr := s.HTTPAddr
	if httpAddr == "" {
		httpAddr = ":http"
	}
	httpL, err := net.Listen("tcp", httpAddr)
	if err != nil {
		return err
	}
	defer httpL.Close() // FIXME multiple close?

	return s.Serve(sshL, httpL)
}

func main() {
	server := &Server{SSHAddr: ":2222", HTTPAddr: ":8080"}

	server.SSHConfig = &ssh.ServerConfig{
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
	server.SSHConfig.AddHostKey(private)

	log.Fatal(server.ListenAndServe())
}
