# tunnel

`tunnel` is a reverse proxy for exposing local web servers to the outside world via SSH.  It is a minimal self-hosted tool in the tradition of web services like [ngrok][1], [localtunnel][2], and [Serveo][3].  Like Serveo, it only requires an SSH implementation on the client; like localtunnel, it is free software.

## Testing it out

A toy installation of `tunnel` running on localhost can be started and tested like this:

    # get it
    go get github.com/alexshpilkin/tunnel
    # generate a host key
    ssh-keygen -f ssh_host_key -t rsa -N ''
    # launch the server
    tunnel --bind-ssh 2222 --bind-http 8080 --authorized-keys ~/.ssh/authorized_keys
    # forward test.localhost to localhost:8000
    ssh -fN -R test.localhost:0:localhost:8000 -p 2222 localhost
    # launch an HTTP server on localhost:8000
    python -m http.server
    # see the result!
    curl -H 'Host:test.localhost' http://test.localhost:8080/

This may seem a bit underwhelming, but running `tunnel` on localhost is kind of pointless.  Normally, you’d want to set it up behind a TLS terminator with a wildcard certificate and a reverse proxy so that it can give out *.yourdomain instead of *.localhost, and expose it on port 80 or 443 so that fiddling with the Host header is not necessary.  The key point is that once a `tunnel` instance has been set up at SERVER, you can use

    ssh -N -R DOMAIN:0:HOST:PORT SERVER

to expose HOST:PORT under DOMAIN.

[1]: https://ngrok.com/
[2]: https://localtunnel.me/
[3]: http://serveo.net/
