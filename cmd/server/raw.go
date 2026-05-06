package main

import (
	"bufio"
	"bytes"
	"io"
	"net"

	"github.com/renat/rinha-backend-2026-go/internal/vector"
)

var (
	rawReadyResponse = []byte("HTTP/1.1 204 No Content\r\nContent-Length: 0\r\nConnection: keep-alive\r\n\r\n")
	rawOKTrue        = []byte("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 33\r\nConnection: keep-alive\r\n\r\n{\"approved\":true,\"fraud_score\":0}")
	rawOKFalse       = []byte("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 34\r\nConnection: keep-alive\r\n\r\n{\"approved\":false,\"fraud_score\":1}")
	rawNotFound      = []byte("HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\nConnection: keep-alive\r\n\r\n")
)

const (
	rawRouteNotFound = iota
	rawRouteReady
	rawRouteFraud
)

func (a *app) serveRaw(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go a.handleRawConn(conn)
	}
}

func (a *app) handleRawConn(conn net.Conn) {
	defer conn.Close()
	br := bufio.NewReaderSize(conn, 4096)
	body := make([]byte, 2048)
	for {
		route, contentLength, err := readRawRequestHeader(br)
		if err != nil {
			return
		}
		if contentLength > cap(body) {
			body = make([]byte, contentLength)
		}
		reqBody := body[:contentLength]
		if contentLength > 0 {
			if _, err := io.ReadFull(br, reqBody); err != nil {
				return
			}
		}
		var resp []byte
		switch route {
		case rawRouteReady:
			resp = rawReadyResponse
		case rawRouteFraud:
			resp = a.rawFraudResponse(reqBody)
		default:
			resp = rawNotFound
		}
		if _, err := conn.Write(resp); err != nil {
			return
		}
	}
}

func readRawRequestHeader(br *bufio.Reader) (int, int, error) {
	line, err := br.ReadSlice('\n')
	if err != nil {
		return 0, 0, err
	}
	route := rawRoute(line)
	contentLength := 0
	for {
		line, err = br.ReadSlice('\n')
		if err != nil {
			return 0, 0, err
		}
		if len(line) == 2 && line[0] == '\r' && line[1] == '\n' {
			return route, contentLength, nil
		}
		if len(line) >= 16 && bytes.EqualFold(line[:14], []byte("Content-Length")) {
			contentLength = parseHeaderInt(line[15:])
		}
	}
}

func rawRoute(line []byte) int {
	first := bytes.IndexByte(line, ' ')
	if first < 0 {
		return rawRouteNotFound
	}
	rest := line[first+1:]
	second := bytes.IndexByte(rest, ' ')
	if second < 0 {
		return rawRouteNotFound
	}
	path := rest[:second]
	if bytes.Equal(path, []byte("/fraud-score")) {
		return rawRouteFraud
	}
	if bytes.Equal(path, []byte("/ready")) {
		return rawRouteReady
	}
	return rawRouteNotFound
}

func parseHeaderInt(s []byte) int {
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}

func (a *app) rawFraudResponse(body []byte) []byte {
	q, ok := vector.VectorizeJSON(body, a.norm, a.mcc)
	if !ok {
		return rawOKTrue
	}
	_, approved := a.decisionForVector(q)
	if approved {
		return rawOKTrue
	}
	return rawOKFalse
}
