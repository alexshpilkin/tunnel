package main

import (
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

type tcpipForward struct {
	Host string
	Port uint32
}

type tcpipForwardSuccess struct {
	Port uint32
}

type cancelTCPIPForward struct {
	Host string
	Port uint32
}

type forwardedTCPIP struct {
	Host       string
	Port       uint32
	OriginHost string
	OriginPort uint32
}

type chanConn struct {
	Chan ssh.Channel
}

func (c *chanConn) Read(b []byte) (int, error) {
	return c.Chan.Read(b)
}

func (c *chanConn) Write(b []byte) (int, error) {
	return c.Chan.Write(b)
}

func (c *chanConn) Close() error {
	return c.Chan.Close()
}

func (c *chanConn) LocalAddr() net.Addr {
	panic("chanConn: LocalAddr")
}

func (c *chanConn) RemoteAddr() net.Addr {
	panic("chanConn: RemoteAddr")
}

func (c *chanConn) SetDeadline(t time.Time) error {
	return nil
}

func (c *chanConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (c *chanConn) SetWriteDeadline(t time.Time) error {
	return nil
}

func isDomainName(s string) bool {
	// Very loose validation to exclude port numbers.  Only for lowercase strings.
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' && r != '.' {
			return false
		}
	}
	return true
}

type Server struct {
	SSHAddr   string
	HTTPAddr  string
	SSHConfig *ssh.ServerConfig
	hosts     sync.Map
}

func (s *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	t, ok := s.hosts.Load(strings.ToLower(req.Host))
	if !ok {
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

	proxy := httputil.ReverseProxy{
		Director:  director,
		Transport: t.(http.RoundTripper),
	}
	proxy.ServeHTTP(w, req)
}

func director(req *http.Request) {
	req.URL.Scheme = "http"
	host := strings.ToLower(req.Host)
	req.URL.Host = host
	req.Host = host
	if _, ok := req.Header["User-Agent"]; !ok {
		req.Header.Set("User-Agent", "")
	}
}

func (s *Server) transport(conn *ssh.ServerConn, host string) http.RoundTripper {
	dial := func(network, addr string) (net.Conn, error) {
		if network != "tcp" && network != "tcp4" && network != "tcp6" {
			panic("invalid network in dial")
		}

		host_, serv, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		if host_ != strings.ToLower(host) {
			panic("invalid host in dial")
		}
		port, err := net.LookupPort(network, serv)
		if err != nil {
			return nil, err
		}
		if port != 80 {
			panic("invalid port in dial")
		}

		payload := forwardedTCPIP{Host: host, Port: 80, OriginHost: "0.0.0.0", OriginPort: 0}
		ch, reqs, err := conn.OpenChannel("forwarded-tcpip", ssh.Marshal(&payload))
		if err != nil {
			return nil, err
		}
		go ssh.DiscardRequests(reqs)
		return &chanConn{ch}, nil
	}
	return &http.Transport{Dial: dial}
}

func (s *Server) serveSSH(l net.Listener) error {
	for {
		rw, err := l.Accept()
		if err != nil {
			log.Print(err)
			continue
		}
		go func() {
			conn, chans, reqs, err := ssh.NewServerConn(rw, s.SSHConfig)
			if err != nil {
				log.Print(err)
				return
			}

			hosts := make(map[string]struct{})
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
						host := strings.ToLower(payload.Host)
						if !isDomainName(host) || payload.Port != 0 && payload.Port != 80 {
							req.Reply(false, nil)
							continue
						}
						transport := s.transport(conn, payload.Host)
						if _, ok := s.hosts.LoadOrStore(host, transport); ok {
							req.Reply(false, nil)
							continue
						}
						hosts[host] = struct{}{}
						replyPayload := tcpipForwardSuccess{Port: 80}
						req.Reply(true, ssh.Marshal(&replyPayload))
					} else if req.Type == "cancel-tcpip-forward" {
						var payload cancelTCPIPForward
						if err := ssh.Unmarshal(req.Payload, &payload); err != nil {
							req.Reply(false, nil)
							continue
						}
						host := strings.ToLower(payload.Host)
						if payload.Port != 80 {
							req.Reply(false, nil)
							continue
						}
						if _, ok := hosts[host]; ok {
							req.Reply(false, nil)
							continue
						}
						s.hosts.Delete(payload.Host)
						delete(hosts, payload.Host)
						req.Reply(true, nil)
					} else {
						req.Reply(false, nil)
					}
				case ch, ok := <-chans:
					if !ok {
						chans = nil
						continue
					}
					ch.Reject(ssh.UnknownChannelType, "No incoming channels accepted")
				}
			}
			for host := range hosts {
				s.hosts.Delete(host)
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
