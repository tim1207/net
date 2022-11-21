package http2

import (
	"bytes"
	"encoding/gob"
	"io"
	"net"

	"github.com/nycu-ucr/gonet/http"
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

type OnvmTransport struct{}

func (t *OnvmTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var err error

	ori_req := req
	println(ori_req)

	conn, err := t.GetConn(req)

	b, err := encodeRequest(req)
	conn.Write(b)

	buf := make([]byte, 4096*2)
	conn.Read(buf)
	resp_wrapper, err := decodeResponse(buf)
	resp_wrapper.Response.Body = io.NopCloser(bytes.NewBuffer(resp_wrapper.Body))

	return resp_wrapper.Response, err
}

func (t *OnvmTransport) GetConn(req *http.Request) (net.Conn, error) {
	// TODO: Use ONVM
	return net.Dial("tcp", req.Host)
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
