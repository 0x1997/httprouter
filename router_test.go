// Copyright 2013 Julien Schmidt. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found
// in the LICENSE file.

package httprouter

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"testing"
	"time"

	"github.com/valyala/fasthttp"
)

type mockConnection struct {
	net.Conn
	r bytes.Buffer
	w bytes.Buffer
}

var zeroTCPAddr = &net.TCPAddr{
	IP: net.IPv4zero,
}

func (rw *mockConnection) Close() error {
	return nil
}

func (rw *mockConnection) Read(b []byte) (int, error) {
	return rw.r.Read(b)
}

func (rw *mockConnection) Write(b []byte) (int, error) {
	return rw.w.Write(b)
}

func (rw *mockConnection) RemoteAddr() net.Addr {
	return zeroTCPAddr
}

func (rw *mockConnection) LocalAddr() net.Addr {
	return zeroTCPAddr
}

func (rw *mockConnection) SetReadDeadline(t time.Time) error {
	return nil
}

func (rw *mockConnection) SetWriteDeadline(t time.Time) error {
	return nil
}

type mockServer struct {
	t      *testing.T
	server *fasthttp.Server
	conn   *mockConnection
	ch     chan error
	br     *bufio.Reader
	resp   fasthttp.Response
}

func newMockServer(t *testing.T, handler fasthttp.RequestHandler) *mockServer {
	conn := &mockConnection{}
	return &mockServer{
		t: t,
		server: &fasthttp.Server{
			Handler: handler,
		},
		conn: conn,
		ch:   make(chan error),
		br:   bufio.NewReader(&conn.w),
		resp: fasthttp.Response{},
	}
}

func (s *mockServer) request(payload string) {
	s.conn.r.WriteString(payload)
	go func() {
		s.ch <- s.server.ServeConn(s.conn)
	}()
	select {
	case err := <-s.ch:
		if err != nil {
			s.t.Fatalf("return error %s", err)
		}
	case <-time.After(100 * time.Millisecond):
		s.t.Fatalf("timeout")
	}
}

func (s *mockServer) readResponse() {
	if err := s.resp.Read(s.br); err != nil {
		s.t.Fatalf("Unexpected error when reading response: %s", err)
	}
}

func TestRouter(t *testing.T) {
	router := New()

	routed := false
	router.Handle("GET", "/user/:name", func(ctx *fasthttp.RequestCtx) {
		routed = true
		want := map[string]string{"name": "gopher"}
		if ctx.UserValue("name") != want["name"] {
			t.Fatalf("wrong wildcard values: want %v, got %v", want["name"], ctx.UserValue("name"))
		}
		ctx.Success("foo/bar", []byte("success"))
	})

	s := newMockServer(t, router.Handler)

	s.request("GET /user/gopher?baz HTTP/1.1\r\n\r\n")
	if !routed {
		t.Fatal("routing failed")
	}
}

func TestRouterAPI(t *testing.T) {
	var get, head, options, post, put, patch, delete, handler bool

	router := New()
	router.GET("/GET", func(ctx *fasthttp.RequestCtx) {
		get = true
	})
	router.HEAD("/GET", func(ctx *fasthttp.RequestCtx) {
		head = true
	})
	router.OPTIONS("/GET", func(ctx *fasthttp.RequestCtx) {
		options = true
	})
	router.POST("/POST", func(ctx *fasthttp.RequestCtx) {
		post = true
	})
	router.PUT("/PUT", func(ctx *fasthttp.RequestCtx) {
		put = true
	})
	router.PATCH("/PATCH", func(ctx *fasthttp.RequestCtx) {
		patch = true
	})
	router.DELETE("/DELETE", func(ctx *fasthttp.RequestCtx) {
		delete = true
	})
	router.Handle("GET", "/Handler", func(ctx *fasthttp.RequestCtx) {
		handler = true
	})

	s := newMockServer(t, router.Handler)

	s.request("GET /GET HTTP/1.1\r\n\r\n")
	if !get {
		t.Error("routing GET failed")
	}

	s.request("HEAD /GET HTTP/1.1\r\n\r\n")
	if !head {
		t.Error("routing HEAD failed")
	}

	s.request("OPTIONS /GET HTTP/1.1\r\n\r\n")
	if !options {
		t.Error("routing OPTIONS failed")
	}

	s.request("POST /POST HTTP/1.1\r\n\r\n")
	if !post {
		t.Error("routing POST failed")
	}

	s.request("PUT /PUT HTTP/1.1\r\n\r\n")
	if !put {
		t.Error("routing PUT failed")
	}

	s.request("PATCH /PATCH HTTP/1.1\r\n\r\n")
	if !patch {
		t.Error("routing PATCH failed")
	}

	s.request("DELETE /DELETE HTTP/1.1\r\n\r\n")
	if !delete {
		t.Error("routing DELETE failed")
	}

	s.request("GET /Handler HTTP/1.1\r\n\r\n")
	if !handler {
		t.Error("routing Handler failed")
	}
}

func TestRouterRoot(t *testing.T) {
	router := New()
	recv := catchPanic(func() {
		router.GET("noSlashRoot", nil)
	})
	if recv == nil {
		t.Fatal("registering path not beginning with '/' did not panic")
	}
}

func TestRouterChaining(t *testing.T) {
	router1 := New()
	router2 := New()
	router1.NotFound = router2.Handler

	fooHit := false
	router1.POST("/foo", func(ctx *fasthttp.RequestCtx) {
		fooHit = true
		ctx.SetStatusCode(fasthttp.StatusOK)
	})

	barHit := false
	router2.POST("/bar", func(ctx *fasthttp.RequestCtx) {
		barHit = true
		ctx.SetStatusCode(fasthttp.StatusOK)
	})

	s := newMockServer(t, router1.Handler)

	s.request("POST /foo HTTP/1.1\r\n\r\n")
	s.readResponse()
	if !(s.resp.Header.StatusCode() == fasthttp.StatusOK && fooHit) {
		t.Errorf("Regular routing failed with router chaining.")
		t.FailNow()
	}

	s.request("POST /bar HTTP/1.1\r\n\r\n")
	s.readResponse()
	if !(s.resp.Header.StatusCode() == fasthttp.StatusOK && barHit) {
		t.Errorf("Chained routing failed with router chaining.")
		t.FailNow()
	}

	s.request("POST /qax HTTP/1.1\r\n\r\n")
	s.readResponse()
	if !(s.resp.Header.StatusCode() == fasthttp.StatusNotFound) {
		t.Errorf("NotFound behavior failed with router chaining.")
		t.FailNow()
	}
}

func TestRouterOPTIONS(t *testing.T) {
	handlerFunc := func(_ *fasthttp.RequestCtx) {}

	router := New()
	router.POST("/path", handlerFunc)

	// test not allowed
	// * (server)
	s := newMockServer(t, router.Handler)

	s.request("OPTIONS * HTTP/1.1\r\nHost:\r\n\r\n")
	s.readResponse()
	if s.resp.Header.StatusCode() != fasthttp.StatusOK {
		t.Errorf("OPTIONS handling failed: Code=%d, Header=%v",
			s.resp.Header.StatusCode(), s.resp.Header.String())
	} else if allow := string(s.resp.Header.Peek("Allow")); allow != "POST, OPTIONS" {
		t.Error("unexpected Allow header value: " + allow)
	}

	// path
	s.request("OPTIONS /path HTTP/1.1\r\n\r\n")
	s.readResponse()
	if s.resp.Header.StatusCode() != fasthttp.StatusOK {
		t.Errorf("OPTIONS handling failed: Code=%d, Header=%v",
			s.resp.Header.StatusCode(), s.resp.Header.String())
	} else if allow := string(s.resp.Header.Peek("Allow")); allow != "POST, OPTIONS" {
		t.Error("unexpected Allow header value: " + allow)
	}

	s.request("OPTIONS /doesnotexist HTTP/1.1\r\n\r\n")
	s.readResponse()
	if !(s.resp.Header.StatusCode() == fasthttp.StatusNotFound) {
		t.Errorf("OPTIONS handling failed: Code=%d, Header=%v",
			s.resp.Header.StatusCode(), s.resp.Header.String())
	}

	// add another method
	router.GET("/path", handlerFunc)

	// test again
	// * (server)
	s.request("OPTIONS * HTTP/1.1\r\n\r\n")
	s.readResponse()
	if s.resp.Header.StatusCode() != fasthttp.StatusOK {
		t.Errorf("OPTIONS handling failed: Code=%d, Header=%v",
			s.resp.Header.StatusCode(), s.resp.Header.String())
	} else if allow := string(s.resp.Header.Peek("Allow")); allow != "POST, GET, OPTIONS" && allow != "GET, POST, OPTIONS" {
		t.Error("unexpected Allow header value: " + allow)
	}

	// path
	s.request("OPTIONS /path HTTP/1.1\r\n\r\n")
	s.readResponse()
	if s.resp.Header.StatusCode() != fasthttp.StatusOK {
		t.Errorf("OPTIONS handling failed: Code=%d, Header=%v",
			s.resp.Header.StatusCode(), s.resp.Header.String())
	} else if allow := string(s.resp.Header.Peek("Allow")); allow != "POST, GET, OPTIONS" && allow != "GET, POST, OPTIONS" {
		t.Error("unexpected Allow header value: " + allow)
	}

	// custom handler
	var custom bool
	router.OPTIONS("/path", func(_ *fasthttp.RequestCtx) {
		custom = true
	})

	// test again
	// * (server)
	s.request("OPTIONS * HTTP/1.1\r\n\r\n")
	s.readResponse()
	if s.resp.Header.StatusCode() != fasthttp.StatusOK {
		t.Errorf("OPTIONS handling failed: Code=%d, Header=%v",
			s.resp.Header.StatusCode(), s.resp.Header.String())
	} else if allow := string(s.resp.Header.Peek("Allow")); allow != "POST, GET, OPTIONS" && allow != "GET, POST, OPTIONS" {
		t.Error("unexpected Allow header value: " + allow)
	}
	if custom {
		t.Error("custom handler called on *")
	}

	// path
	s.request("OPTIONS /path HTTP/1.1\r\n\r\n")
	s.readResponse()
	if s.resp.Header.StatusCode() != fasthttp.StatusOK {
		t.Errorf("OPTIONS handling failed: Code=%d, Header=%v",
			s.resp.Header.StatusCode(), s.resp.Header.String())
	}
	if !custom {
		t.Error("custom handler not called")
	}
}

func TestRouterNotAllowed(t *testing.T) {
	handlerFunc := func(_ *fasthttp.RequestCtx) {}

	router := New()
	router.POST("/path", handlerFunc)

	// test not allowed
	s := newMockServer(t, router.Handler)

	s.request("GET /path HTTP/1.1\r\n\r\n")
	s.readResponse()
	if !(s.resp.Header.StatusCode() == fasthttp.StatusMethodNotAllowed) {
		t.Errorf("NotAllowed handling failed: Code=%d", s.resp.Header.StatusCode())
	} else if allow := string(s.resp.Header.Peek("Allow")); allow != "POST, OPTIONS" {
		t.Error("unexpected Allow header value: " + allow)
	}

	// add another method
	router.DELETE("/path", handlerFunc)
	router.OPTIONS("/path", handlerFunc) // must be ignored

	// test again
	s.request("GET /path HTTP/1.1\r\n\r\n")
	s.readResponse()
	if !(s.resp.Header.StatusCode() == fasthttp.StatusMethodNotAllowed) {
		t.Errorf("NotAllowed handling failed: Code=%d", s.resp.Header.StatusCode())
	} else if allow := string(s.resp.Header.Peek("Allow")); allow != "POST, DELETE, OPTIONS" && allow != "DELETE, POST, OPTIONS" {
		t.Error("unexpected Allow header value: " + allow)
	}

	responseText := "custom method"
	router.MethodNotAllowed = fasthttp.RequestHandler(func(ctx *fasthttp.RequestCtx) {
		ctx.SetStatusCode(fasthttp.StatusTeapot)
		ctx.Write([]byte(responseText))
	})
	s.request("GET /path HTTP/1.1\r\n\r\n")
	s.readResponse()
	if !bytes.Equal(s.resp.Body(), []byte(responseText)) {
		t.Errorf("unexpected response got %q want %q", string(s.resp.Body()), responseText)
	}
	if s.resp.Header.StatusCode() != fasthttp.StatusTeapot {
		t.Errorf("unexpected response code %d want %d", s.resp.Header.StatusCode(), fasthttp.StatusTeapot)
	}
	if allow := string(s.resp.Header.Peek("Allow")); allow != "POST, DELETE, OPTIONS" && allow != "DELETE, POST, OPTIONS" {
		t.Error("unexpected Allow header value: " + allow)
	}
}

func TestRouterNotFound(t *testing.T) {
	handlerFunc := func(_ *fasthttp.RequestCtx) {}

	router := New()
	router.GET("/path", handlerFunc)
	router.GET("/dir/", handlerFunc)
	router.GET("/", handlerFunc)

	testRoutes := []struct {
		route string
		code  int
	}{
		{"/path/", 301},          // TSR -/
		{"/dir", 301},            // TSR +/
		{"/", 200},               // TSR +/
		{"/PATH", 301},           // Fixed Case
		{"/DIR/", 301},           // Fixed Case
		{"/PATH/", 301},          // Fixed Case -/
		{"/DIR", 301},            // Fixed Case +/
		{"/paTh/?name=foo", 301}, // Fixed Case With Params +/
		{"/paTh?name=foo", 301},  // Fixed Case With Params +/
		{"/../path", 200},        // CleanPath
		{"/nope", 404},           // NotFound
	}

	s := newMockServer(t, router.Handler)
	for _, tr := range testRoutes {
		s.request(fmt.Sprintf("GET %s HTTP/1.1\r\n\r\n", tr.route))
		s.readResponse()
		if !(s.resp.Header.StatusCode() == tr.code) {
			t.Errorf("NotFound handling route %s failed: Code=%d want=%d",
				tr.route, s.resp.Header.StatusCode(), tr.code)
		}
	}

	// Test custom not found handler
	var notFound bool
	router.NotFound = fasthttp.RequestHandler(func(ctx *fasthttp.RequestCtx) {
		ctx.SetStatusCode(404)
		notFound = true
	})
	s.request("GET /nope HTTP/1.1\r\n\r\n")
	s.readResponse()
	if !(s.resp.Header.StatusCode() == 404 && notFound == true) {
		t.Errorf("Custom NotFound handler failed: Code=%d, Header=%v", s.resp.Header.StatusCode(), string(s.resp.Header.Peek("Location")))
	}

	// Test other method than GET (want 307 instead of 301)
	router.PATCH("/path", handlerFunc)
	s.request("PATCH /path/ HTTP/1.1\r\n\r\n")
	s.readResponse()
	if !(s.resp.Header.StatusCode() == 307) {
		t.Errorf("Custom NotFound handler failed: Code=%d, Header=%v", s.resp.Header.StatusCode(), string(s.resp.Header.Peek("Location")))
	}

	// Test special case where no node for the prefix "/" exists
	router = New()
	router.GET("/a", handlerFunc)
	s.server.Handler = router.Handler
	s.request("GET / HTTP/1.1\r\n\r\n")
	s.readResponse()
	if !(s.resp.Header.StatusCode() == 404) {
		t.Errorf("NotFound handling route / failed: Code=%d", s.resp.Header.StatusCode())
	}
}

func TestRouterPanicHandler(t *testing.T) {
	router := New()
	panicHandled := false

	router.PanicHandler = func(ctx *fasthttp.RequestCtx, p interface{}) {
		panicHandled = true
	}

	router.Handle("PUT", "/user/:name", func(_ *fasthttp.RequestCtx) {
		panic("oops!")
	})

	defer func() {
		if rcv := recover(); rcv != nil {
			t.Fatal("handling panic failed")
		}
	}()

	s := newMockServer(t, router.Handler)

	s.request("PUT /user/gopher HTTP/1.1\r\n\r\n")
	if !panicHandled {
		t.Fatal("simulating failed")
	}
}

func TestRouterLookup(t *testing.T) {
	routed := false
	wantHandle := func(_ *fasthttp.RequestCtx) {
		routed = true
	}

	router := New()
	ctx := &fasthttp.RequestCtx{}

	// try empty router first
	handle, tsr := router.Lookup("GET", "/nope", ctx)
	if handle != nil {
		t.Fatalf("Got handle for unregistered pattern: %v", handle)
	}
	if tsr {
		t.Error("Got wrong TSR recommendation!")
	}

	// insert route and try again
	router.GET("/user/:name", wantHandle)

	handle, tsr = router.Lookup("GET", "/user/gopher", ctx)
	if handle == nil {
		t.Fatal("Got no handle!")
	} else {
		handle(nil)
		if !routed {
			t.Fatal("Routing failed!")
		}
	}
	if ctx.UserValue("name") != "gopher" {
		t.Error("Param not set!")
	}

	handle, tsr = router.Lookup("GET", "/user/gopher/", ctx)
	if handle != nil {
		t.Fatalf("Got handle for unregistered pattern: %v", handle)
	}
	if !tsr {
		t.Error("Got no TSR recommendation!")
	}

	handle, tsr = router.Lookup("GET", "/nope", ctx)
	if handle != nil {
		t.Fatalf("Got handle for unregistered pattern: %v", handle)
	}
	if tsr {
		t.Error("Got wrong TSR recommendation!")
	}
}

func TestRouterServeFiles(t *testing.T) {
	router := New()

	recv := catchPanic(func() {
		router.ServeFiles("/noFilepath", os.TempDir())
	})
	if recv == nil {
		t.Fatal("registering path not ending with '*filepath' did not panic")
	}
	body := []byte("fake ico")
	ioutil.WriteFile(os.TempDir()+"/favicon.ico", body, 0644)

	router.ServeFiles("/*filepath", os.TempDir())

	s := newMockServer(t, router.Handler)

	s.request("GET /favicon.ico HTTP/1.1\r\n\r\n")
	s.readResponse()
	if s.resp.Header.StatusCode() != 200 {
		t.Fatalf("Unexpected status code %d. Expected %d", s.resp.Header.StatusCode(), 423)
	}
	if !bytes.Equal(s.resp.Body(), body) {
		t.Fatalf("Unexpected body %q. Expected %q", s.resp.Body(), string(body))
	}
}
