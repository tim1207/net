package http2

import (
	"bytes"
	"encoding/gob"
	"io"
	"net"
	"os"
	"strings"
	"time"

	formatter "github.com/antonfisher/nested-logrus-formatter"
	"github.com/nycu-ucr/gonet/http"
	"github.com/nycu-ucr/onvmpoller"
	"github.com/sirupsen/logrus"
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

const (
	LOG_LEVEL = logrus.TraceLevel
)

var (
	logg *logrus.Logger
	Log  *logrus.Entry
)

func init() {
	logg = logrus.New()
	logg.SetReportCaller(false)

	/* Setup Logger */
	SetLogLevel(LOG_LEVEL)

	logg.Formatter = &formatter.Formatter{
		TimestampFormat: time.RFC3339,
		TrimMessages:    true,
		NoFieldsSpace:   true,
		HideKeys:        true,
		FieldsOrder:     []string{"component", "category"},
	}
	NfName := ParseNfName(os.Args[0])
	Log = logg.WithFields(logrus.Fields{"component": "ONVMTRANS", "category": NfName})
}

func SetLogLevel(level logrus.Level) {
	logg.SetLevel(level)
}

func SetReportCaller(enable bool) {
	logg.SetReportCaller(enable)
}

func ParseNfName(args string) string {
	nfName := strings.Split(args, "/")
	return nfName[1]
}

var (
	upgradePreface = []byte(ClientPreface)
)

type RequestWrapper struct {
	*http.Request
	Body []byte
}

type ResponseWrapper struct {
	*http.Response
	Body []byte
}

func EncodeRequest(req *http.Request) ([]byte, error) {
	var buf bytes.Buffer

	req_wrapper := RequestWrapper{Request: req}
	body_buf := make([]byte, req.ContentLength)

	Log.Traceln("Before read request Body\n")
	if req.Body != nil {
		req.Body.Read(body_buf)
		req_wrapper.Body = body_buf
	}
	Log.Tracef("Before encode:\n Request:\n%+v Body:\n%+v\n", req, body_buf)

	req_wrapper.Request.Body = nil
	req_wrapper.Request.GetBody = nil

	enc := gob.NewEncoder(&buf)
	err := enc.Encode(req_wrapper)

	return buf.Bytes(), err
}

func DecodeRequest(buf []byte) (*RequestWrapper, error) {
	var req_wrapper RequestWrapper

	dec := gob.NewDecoder(bytes.NewReader(buf))
	err := dec.Decode(&req_wrapper)
	Log.Tracef("After dencode:\n Request:\n%+v Body:\n%+v\n", req_wrapper.Request, req_wrapper.Body)

	return &req_wrapper, err
}

func DecodeResponse(buf []byte) (*ResponseWrapper, error) {
	var resp_wrapper ResponseWrapper

	dec := gob.NewDecoder(bytes.NewReader(buf))
	err := dec.Decode(&resp_wrapper)

	return &resp_wrapper, err
}

type OnvmClientConn struct {
	conn net.Conn
	req  *http.Request
}

func (occ *OnvmClientConn) WriteClientPreface() error {
	Log.Traceln("nycu-ucr/net/http2/onvm_transport, WriteClientPreface()")
	_, err := occ.conn.Write(upgradePreface)
	return err
}

func (occ *OnvmClientConn) WriteRequest(req *http.Request) error {
	Log.Traceln("nycu-ucr/net/http2/onvm_transport, WriteRequest()")
	occ.req = req
	b, err := EncodeRequest(req)
	if err != nil {
		Log.Errorf("nycu-ucr/net/http2/onvm_transport, EncodeRequest err: %+v", err)
		return err
	}
	_, err = occ.conn.Write(b)

	return err
}

func (occ *OnvmClientConn) ReadResponse() (*http.Response, error) {
	Log.Traceln("nycu-ucr/net/http2/onvm_transport, ReadResponse()")
	buf := make([]byte, 10240)
	n, err := occ.conn.Read(buf)
	if err != nil {
		Log.Errorf("nycu-ucr/net/http2/onvm_transport, ReadResponse()->Read error: %+v", err)
		return nil, err
	}
	Log.Tracef("nycu-ucr/net/http2/onvm_transport, ReadResponse()->Read: %dbytes", n)
	// resp_wrapper, err := DecodeResponse(buf)

	return occ.makeHttpResponse(buf, n)
}

func (occ *OnvmClientConn) makeHttpResponse(b []byte, n int) (*http.Response, error) {
	Log.Traceln("nycu-ucr/net/http2/onvm_transport, makeHttpResponse()")
	rsp := new(http.Response)
	rsp.Request = occ.req
	rsp.Status = ""
	rsp.Proto = "HTTP/2.0"
	rsp.ProtoMajor = 2
	rsp.ProtoMinor = 0
	rsp.Body = io.NopCloser(bytes.NewBuffer(b[:n]))

	return rsp, nil
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
		Log.Errorf("nycu-ucr/net/http2/onvm_transport, GetConn err: %+v", err)
		return nil, err
	}
	defer occ.Close()

	// Send Client Preface
	err = occ.WriteClientPreface()
	if err != nil {
		Log.Errorf("nycu-ucr/net/http2/onvm_transport, WriteClientPreface err: %+v", err)
		return nil, err
	}

	// Send Request
	err = occ.WriteRequest(req)
	if err != nil {
		Log.Errorf("nycu-ucr/net/http2/onvm_transport, WriteRequest err: %+v", err)
		return nil, err
	}

	// Read Response
	rsp, err := occ.ReadResponse()
	if err != nil {
		Log.Errorln("nycu-ucr/net/http2/onvm_transport, RoundTrip not success")
	} else {
		Log.Traceln("nycu-ucr/net/http2/onvm_transport, RoundTrip success")
	}

	return rsp, err
}

func (ot *OnvmTransport) GetConn(req *http.Request) (*OnvmClientConn, error) {
	var conn net.Conn
	var err error
	if ot.UseONVM {
		Log.Infoln("nycu-ucr/net/http2/onvm_transport, use ONVM")
		conn, err = onvmpoller.DialONVM("onvm", req.Host)
	} else {
		Log.Infoln("nycu-ucr/net/http2/onvm_transport, use TCP")
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
