package http2

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"io"
	"net"
	urlpkg "net/url"

	"github.com/nycu-ucr/gonet/http"
	"github.com/nycu-ucr/onvmpoller"
)

// type SmallRequest struct {
// 	Method           string
// 	URL              *url.URL
// 	Proto            string // "HTTP/1.0"
// 	ProtoMajor       int    // 1
// 	ProtoMinor       int    // 0
// 	Header           http.Header
// 	ContentLength    int64
// 	Host             string
// 	BodyByte         []byte
// 	Close            bool
// 	TransferEncoding []string
// 	Form             url.Values
// 	PostForm         url.Values
// 	MultipartForm    *multipart.Form
// 	Trailer          http.Header
// 	RemoteAddr       string
// 	RequestURI       string
// 	TLS              *tls.ConnectionState
// 	Cancel           <-chan struct{}
// 	Response         *http.Response
// 	// ctx              context.Context
// 	// GetBody       func() (io.ReadCloser, error)
// 	// Body          io.ReadCloser
// }

// type Response struct {
// 	Status           string // e.g. "200 OK"
// 	StatusCode       int    // e.g. 200
// 	Proto            string // e.g. "HTTP/1.0"
// 	ProtoMajor       int    // e.g. 1
// 	ProtoMinor       int    // e.g. 0
// 	Header           Header
// 	Body             io.ReadCloser
// 	ContentLength    int64
// 	TransferEncoding []string
// 	Close            bool
// 	Uncompressed     bool
// 	Trailer          Header
// 	Request          *Request
// 	TLS              *tls.ConnectionState
// }

type RequestWrapper struct {
	*http.Request
	Body []byte
}

type ResponseWrapper struct {
	*http.Response
	Body []byte
}

func encodeRequest(req *http.Request) ([]byte, error) {
	var buf bytes.Buffer

	body_buf := make([]byte, req.ContentLength)
	req.Body.Read(body_buf)

	req_wrapper := RequestWrapper{Request: req, Body: body_buf}
	req_wrapper.Request.Body = nil
	req_wrapper.Request.GetBody = nil

	enc := gob.NewEncoder(&buf)
	err := enc.Encode(req_wrapper)

	return buf.Bytes(), err
}

func decodeRequest(buf []byte) (*RequestWrapper, error) {
	var req_wrapper RequestWrapper

	dec := gob.NewDecoder(bytes.NewReader(buf))
	err := dec.Decode(&req_wrapper)

	return &req_wrapper, err
}

func decodeResponse(buf []byte) (*ResponseWrapper, error) {
	var resp_wrapper ResponseWrapper

	dec := gob.NewDecoder(bytes.NewReader(buf))
	err := dec.Decode(&resp_wrapper)

	return &resp_wrapper, err
}

type OnvmClientConn struct {
	conn net.Conn
}

func (occ *OnvmClientConn) WriteClientPreface() {
	req := &http.Request{
		Method: "PRI",
		URL:    &urlpkg.URL{Path: "*"},
		Proto:  "HTTP/2.0",
		Body:   io.NopCloser(bytes.NewReader([]byte("SM\r\n\r\n"))),
	}

	occ.WriteRequest(req)
}

func (occ *OnvmClientConn) WriteRequest(req *http.Request) error {
	b, err := encodeRequest(req)
	occ.conn.Write(b)

	return err
}

func (occ *OnvmClientConn) ReadResponse() (*http.Response, error) {
	buf := make([]byte, 16<<10)
	occ.conn.Read(buf)
	resp_wrapper, err := decodeResponse(buf)
	resp_wrapper.Response.Body = io.NopCloser(bytes.NewBuffer(resp_wrapper.Body))

	return resp_wrapper.Response, err
}

func (occ *OnvmClientConn) Close() {
	occ.conn.Close()
}

type OnvmTransport struct {
	UseONVM bool
}

func (ot *OnvmTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var err error

	// Get Connection
	occ, err := ot.GetConn(req)
	if err != nil {
		return nil, err
	}
	defer occ.Close()

	// Send Client Preface
	occ.WriteClientPreface()

	// Send Request
	err = occ.WriteRequest(req)
	if err != nil {
		return nil, err
	}

	// Read Response
	return occ.ReadResponse()
}

func (ot *OnvmTransport) GetConn(req *http.Request) (*OnvmClientConn, error) {
	var conn net.Conn
	var err error
	if ot.UseONVM {
		fmt.Println("net/http2/onvm_transport, use ONVM")
		conn, err = onvmpoller.DialONVM("onvm", req.Host)
	} else {
		fmt.Println("net/http2/onvm_transport, use TCP")
		conn, err = net.Dial("tcp", req.Host)
	}
	if err != nil {
		return nil, err
	}

	return &OnvmClientConn{conn: conn}, err
}

func WriteHeader(req *http.Request) {}

func WriteData(req *http.Request) {}

func WriteRequest(req *http.Request) {
	// Generate Header

	WriteHeader(req)

	if req.Body != nil {
		WriteData(req)
	}
}

func ReadFrame() {}

func ReadResponse() (resp *http.Response, err error) {
	// Read Frame

	// Parse Frame

	return
}
