package http2

import (
	"context"
	"io"
	"net"
	"runtime"

	"github.com/nycu-ucr/gonet/http"
)

/*
type http.ResponseWriter interface {
	Header() Header

	Write([]byte) (int, error)

	WriteHeader(statusCode int)
}


[ Offical http2 ResponseWriter method ]
func (*ResponseWriter).CloseNotify() <-chan bool
func (*ResponseWriter).Flush()
func (*ResponseWriter).Header() http.Header
func (*ResponseWriter).Push(target string, opt *http.PushOptions) error
func (*ResponseWriter).Write(p []byte) (n int, err error)
func (*ResponseWriter).WriteHeader(code int)
func (*ResponseWriter).WriteString(s string) (n int, err error)
*/

type onvmConn struct {
	// Immutable:
	srv           *Server
	hs            *http.Server
	conn          net.Conn
	handler       http.Handler
	baseCtx       context.Context
	remoteAddrStr string
	// doneServing      chan struct{}          // closed when serverConn.serve ends
	// readFrameCh      chan readFrameResult   // written by serverConn.readFrames
	// wantWriteFrameCh chan FrameWriteRequest // from handlers -> serve
	// wroteFrameCh     chan frameWriteResult  // from writeFrameAsync -> serve, tickles more frame writes
	// bodyReadCh       chan bodyReadMsg       // from handlers -> serve
	// serveMsgCh       chan interface{}       // misc messages & code to send to / run on the serve loop
	/* Test */
	handlerPanicCh chan struct{}
	readConnErrCh  chan error
	reqDecodeErrCh chan error
	httpRequestCh  chan *http.Request
}

func (s *Server) ServeOnvmConn(c net.Conn, opts *ServeConnOpts) {
	// Log.Tracef("nycu-ucr/net/http2/onvm_server.go/ServeOnvmConn")
	baseCtx, cancel := serverConnBaseContext(c, opts)
	defer cancel()

	oc := &onvmConn{
		srv:           s,
		hs:            opts.baseConfig(),
		conn:          c,
		baseCtx:       baseCtx,
		remoteAddrStr: c.RemoteAddr().String(),
		handler:       opts.handler(),
		// readFrameCh:      make(chan readFrameResult),
		// wantWriteFrameCh: make(chan FrameWriteRequest, 8),
		// serveMsgCh:       make(chan interface{}, 8),
		// wroteFrameCh:     make(chan frameWriteResult, 1), // buffered; one send in writeFrameAsync
		// bodyReadCh:       make(chan bodyReadMsg),         // buffering doesn't matter either way
		// doneServing:      make(chan struct{}),
		handlerPanicCh: make(chan struct{}),
		readConnErrCh:  make(chan error),
		reqDecodeErrCh: make(chan error),
		httpRequestCh:  make(chan *http.Request, 1),
	}

	// buff := make([]byte, 10240)
	// n, err := oc.conn.Read(buff)

	// if err != nil {
	// 	Log.Errorf("nycu-ucr/net/http2/server.go/ServeOnvmConn: net.Conn.Read error -> %+v\n", err)
	// }
	// // Log.Tracef("nycu-ucr/net/http2/server.go/ServeOnvmConn: net.Conn.Read -> %d bytes\n", n)

	// req, err := FastDecodeRequest(buff)

	// st := sc.newStream(0, 0, stateOpen)
	// req.Request.Body = io.NopCloser(bytes.NewReader(req.Body))

	// rw := sc.onvm_newResponseWriter(req.Request)
	// onvmrw := sc.newOnvmResponseWriter(req.Request)
	// onvmrw := oc.newOnvmResponseWriter(req)

	// sc.onvm_runHandler(rw, req.Request, sc.handler.ServeHTTP)
	// sc.onvmRunHandler(onvmrw, req.Request, sc.handler.ServeHTTP)
	// oc.onvmRunHandler(onvmrw, req, oc.handler.ServeHTTP)

	oc.serve()
	// Log.Infoln("nycu-ucr/net/http2/server.go/ServeOnvmConn [Done]")
}

func (oc *onvmConn) serve() {
	defer oc.conn.Close()

	go oc.readRequest()

	loopNum := 0
	for {
		loopNum++
		select {
		case req := <-oc.httpRequestCh:
			onvmrw := oc.newOnvmResponseWriter(req)
			oc.onvmRunHandler(onvmrw, req, oc.handler.ServeHTTP)
		case <-oc.handlerPanicCh:
			oc.conn.Close()
			return
		case err := <-oc.readConnErrCh:
			if err == io.EOF {
				oc.conn.Close()
			}
			return
		}
	}
}

func (oc *onvmConn) readRequest() {
	for {
		buff := make([]byte, 10240)
		n, err := oc.conn.Read(buff)

		if err != nil {
			Log.Errorf("nycu-ucr/net/http2/server.go/ServeOnvmConn: net.Conn.Read error -> %+v\n", err)
			oc.readConnErrCh <- err
			return
		}
		// Log.Tracef("nycu-ucr/net/http2/server.go/ServeOnvmConn: net.Conn.Read -> %d bytes\n", n)

		if n != 0 {
			req, err := FastDecodeRequest(buff)
			if err != nil {
				Log.Errorf("nycu-ucr/net/http2/server.go/ServeOnvmConn: FastDecodeRequest error -> %+v\n", err)
				oc.readConnErrCh <- err
				return
			} else {
				oc.httpRequestCh <- req
			}
		}
	}
}

/* Get onvm base offical http2 http.ResponseWriter */
func (oc *onvmConn) newOnvmResponseWriter(req *http.Request) *onvmresponseWriter {
	Log.Traceln("nycu-ucr/net/http2/onvm_server.go/newOnvmResponseWriter")
	rws := new(onvmresponseWriterState)
	rws.onvmConn = oc
	rws.req = req
	return &onvmresponseWriter{rws: rws}
}

/* Use onvm base http.ResponseWriter to run ServeHTTP */
func (oc *onvmConn) onvmRunHandler(onvmrw *onvmresponseWriter, req *http.Request, handler func(http.ResponseWriter, *http.Request)) {
	Log.Traceln("nycu-ucr/net/http2/onvm_server.go/onvmRunHandler [Start]\n")
	didPanic := true
	defer func() {
		if didPanic {
			e := recover()
			// Same as net/http:
			if e != nil && e != http.ErrAbortHandler {
				const size = 64 << 10
				buf := make([]byte, size)
				buf = buf[:runtime.Stack(buf, false)]
				Log.Errorf("nycu-ucr/net/http2/onvm_server.go/onvmRunHandler panic serving %v: %v\n%s", oc.conn.RemoteAddr(), e, buf)
			}
			oc.handlerPanicCh <- struct{}{}
			return
		}
		onvmrw.rws.handlerDone = true
	}()
	handler(onvmrw, req)
	didPanic = false
	Log.Traceln("nycu-ucr/net/http2/onvm_server.go/onvmRunHandler [End]\n")
}

/*********************************
  Methods of http.ResponseWriter
*********************************/

type onvmresponseWriter struct {
	rws *onvmresponseWriterState
}

type onvmresponseWriterState struct {
	req      *http.Request
	onvmConn *onvmConn

	// mutated by http.Handler goroutine:
	handlerHeader http.Header // nil until called
	snapHeader    http.Header // snapshot of handlerHeader at WriteHeader time
	trailers      []string    // set in writeChunk
	status        int         // status code passed to WriteHeader
	wroteHeader   bool        // WriteHeader called (explicitly or implicitly). Not necessarily sent to user yet.
	sentHeader    bool        // have we sent the header frame?
	handlerDone   bool        // handler has finished

	sentContentLen int64 // non-zero if handler set a Content-Length header
	wroteBytes     int64
}

func (w *onvmresponseWriter) Header() http.Header {
	// Log.Traceln("nycu-ucr/net/http2/server.go, (*onvmresponseWriter).Header")
	rws := w.rws
	if rws == nil {
		panic("Header called after Handler finished")
	}
	if rws.handlerHeader == nil {
		rws.handlerHeader = make(http.Header)
	}
	return rws.handlerHeader
}

func (w *onvmresponseWriter) WriteHeader(code int) {
	// Log.Traceln("nycu-ucr/net/http2/server.go, (*onvmresponseWriter).WriteHeader")
	rws := w.rws
	if rws == nil {
		panic("WriteHeader called after Handler finished")
	}
	rws.status = code
}

func (w *onvmresponseWriter) Write(p []byte) (n int, err error) {
	// Log.Traceln("nycu-ucr/net/http2/server.go, (*onvmresponseWriter).Write")
	rws := w.rws
	if rws == nil {
		panic("Write called after Handler finished")
	}
	if !bodyAllowedForStatus(rws.status) {
		return 0, http.ErrBodyNotAllowed
	}

	// Log.Tracef("nycu-ucr/net/http2/server.go, (*onvmresponseWriter).Write:\n%s\n", string(p))
	b, err := FastEncodeResponse(int32(rws.status), w.Header(), int64(len(p)), p)
	if err != nil {
		Log.Errorf("nycu-ucr/net/http2/onvm_server, EncodeRepose err: %+v", err)
		return 0, err
	}
	n, err = rws.onvmConn.conn.Write(b)

	return n, err
}
