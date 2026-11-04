#!/usr/bin/env python3
"""tls-server.py - an HTTPS file server for net-smoke (W17-S06): serves a
directory on 127.0.0.1:<port> with a self-signed certificate, so gofetch on
Burrow can do a TLS handshake without the internet (with -k; without it the
certificate must be refused). Each connection's handshake runs in its own
thread with a timeout, so a client that gives up halfway (the refused one)
cannot hold up the next. Never listens beyond loopback.

Usage: tls-server.py <port> <dir> <cert.pem> <key.pem>
"""
import functools
import http.server
import ssl
import sys

HANDSHAKE_S = 30


class TLSServer(http.server.ThreadingHTTPServer):
    def get_request(self):
        sock, addr = self.socket.accept()
        sock.settimeout(HANDSHAKE_S)
        # the handshake happens at the handler thread's first read
        return self.ctx.wrap_socket(sock, server_side=True, do_handshake_on_connect=False), addr


port, root, cert, key = int(sys.argv[1]), sys.argv[2], sys.argv[3], sys.argv[4]
handler = functools.partial(http.server.SimpleHTTPRequestHandler, directory=root)
server = TLSServer(("127.0.0.1", port), handler)
server.ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
server.ctx.load_cert_chain(cert, key)
server.serve_forever()
