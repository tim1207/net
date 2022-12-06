package http2

import (
	"bytes"
	"fmt"
	"io"
	"math"
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

func (s *Server) ServeOnvmConn(c net.Conn, opts *ServeConnOpts) {
	fmt.Println("nycu-ucr/net/http2/onvm_server.go/ServeOnvmConn")
	baseCtx, cancel := serverConnBaseContext(c, opts)
	defer cancel()

	sc := &serverConn{
		srv:                         s,
		hs:                          opts.baseConfig(),
		conn:                        c,
		baseCtx:                     baseCtx,
		remoteAddrStr:               c.RemoteAddr().String(),
		bw:                          newBufferedWriter(c),
		handler:                     opts.handler(),
		streams:                     make(map[uint32]*stream),
		readFrameCh:                 make(chan readFrameResult),
		wantWriteFrameCh:            make(chan FrameWriteRequest, 8),
		serveMsgCh:                  make(chan interface{}, 8),
		wroteFrameCh:                make(chan frameWriteResult, 1), // buffered; one send in writeFrameAsync
		bodyReadCh:                  make(chan bodyReadMsg),         // buffering doesn't matter either way
		doneServing:                 make(chan struct{}),
		clientMaxStreams:            math.MaxUint32, // Section 6.5.2: "Initially, there is no limit to this value"
		advMaxStreams:               s.maxConcurrentStreams(),
		initialStreamSendWindowSize: initialWindowSize,
		maxFrameSize:                initialMaxFrameSize,
		headerTableSize:             initialHeaderTableSize,
		serveG:                      newGoroutineLock(),
		pushEnabled:                 true,
		sawClientPreface:            opts.SawClientPreface,
	}

	s.state.registerConn(sc)
	defer s.state.unregisterConn(sc)

	// // The net/http package sets the write deadline from the
	// // http.Server.WriteTimeout during the TLS handshake, but then
	// // passes the connection off to us with the deadline already set.
	// // Write deadlines are set per stream in serverConn.newStream.
	// // Disarm the net.Conn write deadline here.
	// if sc.hs.WriteTimeout != 0 {
	// 	sc.conn.SetWriteDeadline(time.Time{})
	// }

	// if s.NewWriteScheduler != nil {
	// 	sc.writeSched = s.NewWriteScheduler()
	// } else {
	// 	sc.writeSched = NewPriorityWriteScheduler(nil)
	// }

	// // These start at the RFC-specified defaults. If there is a higher
	// // configured value for inflow, that will be updated when we send a
	// // WINDOW_UPDATE shortly after sending SETTINGS.
	// sc.flow.add(initialWindowSize)
	// sc.inflow.add(initialWindowSize)
	// sc.hpackEncoder = hpack.NewEncoder(&sc.headerWriteBuf)

	// fr := NewFramer(sc.bw, c)
	// if s.CountError != nil {
	// 	fr.countError = s.CountError
	// }
	// fr.ReadMetaHeaders = hpack.NewDecoder(initialHeaderTableSize, nil)
	// fr.MaxHeaderListSize = sc.maxHeaderListSize()
	// fr.SetMaxReadFrameSize(s.maxReadFrameSize())
	// sc.framer = fr

	// if tc, ok := c.(connectionStater); ok {
	// 	sc.tlsState = new(tls.ConnectionState)
	// 	*sc.tlsState = tc.ConnectionState()
	// 	// 9.2 Use of TLS Features
	// 	// An implementation of HTTP/2 over TLS MUST use TLS
	// 	// 1.2 or higher with the restrictions on feature set
	// 	// and cipher suite described in this section. Due to
	// 	// implementation limitations, it might not be
	// 	// possible to fail TLS negotiation. An endpoint MUST
	// 	// immediately terminate an HTTP/2 connection that
	// 	// does not meet the TLS requirements described in
	// 	// this section with a connection error (Section
	// 	// 5.4.1) of type INADEQUATE_SECURITY.
	// 	if sc.tlsState.Version < tls.VersionTLS12 {
	// 		sc.rejectConn(ErrCodeInadequateSecurity, "TLS version too low")
	// 		return
	// 	}

	// 	if sc.tlsState.ServerName == "" {
	// 		// Client must use SNI, but we don't enforce that anymore,
	// 		// since it was causing problems when connecting to bare IP
	// 		// addresses during development.
	// 		//
	// 		// TODO: optionally enforce? Or enforce at the time we receive
	// 		// a new request, and verify the ServerName matches the :authority?
	// 		// But that precludes proxy situations, perhaps.
	// 		//
	// 		// So for now, do nothing here again.
	// 	}

	// 	if !s.PermitProhibitedCipherSuites && isBadCipher(sc.tlsState.CipherSuite) {
	// 		// "Endpoints MAY choose to generate a connection error
	// 		// (Section 5.4.1) of type INADEQUATE_SECURITY if one of
	// 		// the prohibited cipher suites are negotiated."
	// 		//
	// 		// We choose that. In my opinion, the spec is weak
	// 		// here. It also says both parties must support at least
	// 		// TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256 so there's no
	// 		// excuses here. If we really must, we could allow an
	// 		// "AllowInsecureWeakCiphers" option on the server later.
	// 		// Let's see how it plays out first.
	// 		sc.rejectConn(ErrCodeInadequateSecurity, fmt.Sprintf("Prohibited TLS 1.2 Cipher Suite: %x", sc.tlsState.CipherSuite))
	// 		return
	// 	}
	// }

	// if opts.Settings != nil {
	// 	fr := &SettingsFrame{
	// 		FrameHeader: FrameHeader{valid: true},
	// 		p:           opts.Settings,
	// 	}
	// 	if err := fr.ForeachSetting(sc.processSetting); err != nil {
	// 		sc.rejectConn(ErrCodeProtocol, "invalid settings")
	// 		return
	// 	}
	// 	opts.Settings = nil
	// }

	// if hook := testHookGetServerConn; hook != nil {
	// 	hook(sc)
	// }

	// if opts.UpgradeRequest != nil {
	// 	sc.upgradeRequest(opts.UpgradeRequest)
	// 	opts.UpgradeRequest = nil
	// }

	buff := make([]byte, 10240)
	n, err := sc.conn.Read(buff)

	if err != nil {
		fmt.Printf("nycu-ucr/net/http2/server.go/ServeOnvmConn: net.Conn.Read error -> %+v\n", err)
	}
	fmt.Printf("nycu-ucr/net/http2/server.go/ServeOnvmConn: net.Conn.Read -> %d bytes\n", n)

	req, err := DecodeRequest(buff)

	// st := sc.newStream(0, 0, stateOpen)
	req.Request.Body = io.NopCloser(bytes.NewReader(req.Body))

	// rw := sc.onvm_newResponseWriter(req.Request)
	onvmrw := sc.newOnvmResponseWriter(req.Request)

	// sc.onvm_runHandler(rw, req.Request, sc.handler.ServeHTTP)
	sc.onvmRunHandler(onvmrw, req.Request, sc.handler.ServeHTTP)

	// sc.serve()
}

/* Get golang offical http2 http.ResponseWriter */
func (sc *serverConn) onvm_newResponseWriter(req *http.Request) *responseWriter {
	sc.logf("nycu-ucr/net/http2/onvm_server.go/onvm_newResponseWriter")
	rws := responseWriterStatePool.Get().(*responseWriterState)
	bwSave := rws.bw
	*rws = responseWriterState{} // zero all the fields
	rws.conn = sc
	rws.bw = bwSave
	rws.bw.Reset(chunkWriter{rws})
	rws.stream = nil
	rws.req = req
	return &responseWriter{rws: rws}
}

/* Use golang offical http2 http.ResponseWriter to run ServeHTTP */
func (sc *serverConn) onvm_runHandler(rw *responseWriter, req *http.Request, handler func(http.ResponseWriter, *http.Request)) {
	didPanic := true
	defer func() {
		rw.rws.stream.cancelCtx()
		if req.MultipartForm != nil {
			req.MultipartForm.RemoveAll()
		}
		if didPanic {
			e := recover()
			sc.writeFrameFromHandler(FrameWriteRequest{
				write:  handlerPanicRST{rw.rws.stream.id},
				stream: rw.rws.stream,
			})
			// Same as net/http:
			if e != nil && e != http.ErrAbortHandler {
				const size = 64 << 10
				buf := make([]byte, size)
				buf = buf[:runtime.Stack(buf, false)]
				sc.logf("http2: panic serving %v: %v\n%s", sc.conn.RemoteAddr(), e, buf)
			}
			return
		}
		rw.handlerDone()
	}()
	handler(rw, req)
	didPanic = false
}

/* Get onvm base offical http2 http.ResponseWriter */
func (sc *serverConn) newOnvmResponseWriter(req *http.Request) *onvmresponseWriter {
	sc.logf("nycu-ucr/net/http2/onvm_server.go/newOnvmResponseWriter")
	rws := new(onvmresponseWriterState)
	rws.conn = sc
	rws.req = req
	return &onvmresponseWriter{rws: rws}
}

/* Use onvm base http.ResponseWriter to run ServeHTTP */
func (sc *serverConn) onvmRunHandler(onvmrw *onvmresponseWriter, req *http.Request, handler func(http.ResponseWriter, *http.Request)) {
	sc.logf("nycu-ucr/net/http2/onvm_server.go/onvmRunHandler [Start]\n")
	didPanic := true
	defer func() {
		if didPanic {
			e := recover()
			// Same as net/http:
			if e != nil && e != http.ErrAbortHandler {
				const size = 64 << 10
				buf := make([]byte, size)
				buf = buf[:runtime.Stack(buf, false)]
				sc.logf("nycu-ucr/net/http2/onvm_server.go/onvmRunHandler panic serving %v: %v\n%s", sc.conn.RemoteAddr(), e, buf)
			}
			return
		}
		onvmrw.rws.handlerDone = true
	}()
	handler(onvmrw, req)
	didPanic = false
	sc.logf("nycu-ucr/net/http2/onvm_server.go/onvmRunHandler [End]\n")
}

/*********************************
  Methods of http.ResponseWriter
*********************************/

type onvmresponseWriter struct {
	rws *onvmresponseWriterState
}

type onvmresponseWriterState struct {
	req  *http.Request
	conn *serverConn

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
	println("nycu-ucr/net/http2/server.go, (*onvmresponseWriter).Header")
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
	println("nycu-ucr/net/http2/server.go, (*onvmresponseWriter).WriteHeader")
	rws := w.rws
	if rws == nil {
		panic("WriteHeader called after Handler finished")
	}
	rws.status = code
}

func (w *onvmresponseWriter) Write(p []byte) (n int, err error) {
	println("nycu-ucr/net/http2/server.go, (*onvmresponseWriter).Write")
	rws := w.rws
	if rws == nil {
		panic("Write called after Handler finished")
	}
	if !rws.wroteHeader {
		w.WriteHeader(200)
	}
	if !bodyAllowedForStatus(rws.status) {
		return 0, http.ErrBodyNotAllowed
	}

	rws.conn.logf("nycu-ucr/net/http2/server.go, (*onvmresponseWriter).Write:\n%s\n", string(p))
	n, err = rws.conn.conn.Write(p)

	return n, err
}
