package http2

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net/url"

	"github.com/nycu-ucr/gonet/http"
)

/*
type Request struct {
	Method           string
	URL              *url.URL
	Proto            string // "HTTP/1.0"
	ProtoMajor       int    // 1
	ProtoMinor       int    // 0
	Header           http.Header
	ContentLength    int64
	Host             string
	BodyByte         []byte
	Close            bool
	TransferEncoding []string
	Form             url.Values
	PostForm         url.Values
	MultipartForm    *multipart.Form
	Trailer          http.Header
	RemoteAddr       string
	RequestURI       string
	TLS              *tls.ConnectionState
	Cancel           <-chan struct{}
	Response         *http.Response
	// ctx              context.Context
	// GetBody       func() (io.ReadCloser, error)
	// Body          io.ReadCloser
}

type Response struct {
	Status           string // e.g. "200 OK"
	StatusCode       int    // e.g. 200
	Proto            string // e.g. "HTTP/1.0"
	ProtoMajor       int    // e.g. 1
	ProtoMinor       int    // e.g. 0
	Header           Header
	Body             io.ReadCloser
	ContentLength    int64
	TransferEncoding []string
	Close            bool
	Uncompressed     bool
	Trailer          Header
	Request          *Request
	TLS              *tls.ConnectionState
}
*/

/*
*********************************
********** ONVM PDU *************
*********************************

+-----------------------------------------------+
|         Header Length (24)                    |
+---------------+---------------+---------------+
|   Type (8)    |   Flags (8)   |
+-+-------------+---------------+-------------------------------+
|R|                 Stream Identifier (31)                      |
+=+=============================================================+
|                   Header Payload (0...)                     ...      <=  Using TLV,TLV,TLV,...
+---------------------------------------------------------------+
*/

const (
	METHOD      = int8(1) // string
	URL         = int8(2) // string
	STATUS      = int8(3) // int32
	PROTO       = int8(4) // string
	CONTENT_LEN = int8(5) // int64
	HOST        = int8(6) // string
	REMOTE_ADDR = int8(7) // string
	REQUEST_URI = int8(8) // string
	PAYLOAD     = int8(9) // []byte (must put as the last TLV if exist)
)

/* Use for convert between request/response and []byte */
type OnvmPDU struct {
	Buffer  *bytes.Buffer // read from conn or write to conn
	payload []byte
}

type TLV struct {
	Tag    int8
	Length uint32
	Value  any
}

/* Big endian */
func uint64Tobyte(v uint64) (buff []byte) {
	b := make([]byte, 8)
	b[0] = byte(v >> 56)
	b[1] = byte(v >> 48)
	b[2] = byte(v >> 40)
	b[3] = byte(v >> 32)
	b[4] = byte(v >> 24)
	b[5] = byte(v >> 16)
	b[6] = byte(v >> 8)
	b[7] = byte(v)

	return b
}

/* Big endian */
func byteTouint64(b []byte) uint64 {
	return binary.BigEndian.Uint64(b)
}

/* Big endian */
func uint32Tobyte(v uint32) (buff []byte) {
	b := make([]byte, 4)
	b[0] = byte(v >> 24)
	b[1] = byte(v >> 16)
	b[2] = byte(v >> 8)
	b[3] = byte(v)

	return b
}

/* Big endian */
func byteTouint32(b []byte) uint32 {
	return binary.BigEndian.Uint32(b)
}

func (pdu *OnvmPDU) EncodeTLV(tag int8, data any) error {
	Log.Traceln("nycu-ucr/net/http2/onvm_encode_decode, EncodeTLV")
	buf := pdu.Buffer

	// tag
	bt := make([]byte, 1)
	bt[0] = byte(tag)
	if _, err := buf.Write(bt); err != nil {
		Log.Errorf("[EncodeTLV]\nTag: %d\nError: %+v\n", tag, err)
		return err
	}

	// length
	switch v := data.(type) {
	case string:
		if _, err := buf.Write(uint32Tobyte(uint32(len(v)))); err != nil {
			Log.Errorf("[EncodeTLV]\nValue: %s\nError: %+v\n", v, err)
			return err
		}
	case int64:
		if _, err := buf.Write(uint32Tobyte(8)); err != nil {
			Log.Errorf("[EncodeTLV]\nValue: %d\nError: %+v\n", v, err)
			return err
		}
	case int32:
		if _, err := buf.Write(uint32Tobyte(4)); err != nil {
			fmt.Printf("[EncodeTLV]\nValue: %d\nError: %+v\n", v, err)
			return err
		}
	case []byte:
		/* When no Body exist, we still fill the payload tag and its length equeal 0, then return with no value */
		if len(v) == 0 {
			return nil
		}
		if _, err := buf.Write(uint32Tobyte(uint32(len(v)))); err != nil {
			Log.Errorf("[EncodeTLV]\nValue: %d\nError: %+v\n", v, err)
			return err
		}
	}

	// value
	switch v := data.(type) {
	case string:
		if _, err := buf.Write([]byte(v)); err != nil {
			Log.Errorf("[EncodeTLV]\nValue: %s\nError: %+v\n", v, err)
			return err
		}
	case int64:
		if _, err := buf.Write(uint64Tobyte(uint64(v))); err != nil {
			Log.Errorf("[EncodeTLV]\nValue: %d\nError: %+v\n", v, err)
			return err
		}
	case int32:
		if _, err := buf.Write(uint32Tobyte(uint32(v))); err != nil {
			Log.Errorf("[EncodeTLV]\nValue: %d\nError: %+v\n", v, err)
			return err
		}
	case []byte:
		if _, err := buf.Write(v); err != nil {
			Log.Errorf("[EncodeTLV]\nValue: %d\nError: %+v\n", v, err)
			return err
		}
	}

	return nil
}

func (pdu *OnvmPDU) DecodeTLV() (*TLV, error) {
	Log.Traceln("nycu-ucr/net/http2/onvm_encode_decode, DecodeTLV")
	var tlv TLV
	buf := pdu.Buffer

	// tag
	bt := make([]byte, 1)
	_, err := buf.Read(bt)
	if err != nil {
		Log.Errorf("[DecodeTLV][Read tag][Error]: %+v\n", err)
		return &tlv, err
	}
	tlv.Tag = int8(bt[0])

	// length
	bl := make([]byte, 4)
	_, err = buf.Read(bl)
	if err != nil {
		Log.Errorf("[DecodeTLV][Read length][Error]: %+v\n", err)
		return &tlv, err
	}
	tlv.Length = byteTouint32(bl)

	// value
	if tlv.Length != 0 {
		bv := make([]byte, tlv.Length)
		_, err = buf.Read(bv)
		if err != nil {
			Log.Errorf("[DecodeTLV][Read value][Error]: %+v\n", err)
			return &tlv, err
		}
		switch tlv.Tag {
		case STATUS:
			// int32
			tlv.Value = byteTouint32(bv)
		case CONTENT_LEN:
			//int64
			tlv.Value = byteTouint64(bv)
		case PAYLOAD:
			// []byte
			tlv.Value = bv
		default:
			//string
			tlv.Value = string(bv)
		}
	}

	return &tlv, err
}

/*********************************
     Methods of http.Request
*********************************/

func FastEncodeRequest(req *http.Request) ([]byte, error) {
	Log.Traceln("nycu-ucr/net/http2/onvm_encode_decode, FastEncodeRequest")
	var err error
	pdu := &OnvmPDU{
		Buffer: new(bytes.Buffer),
	}

	if req.ContentLength != 0 {
		pdu.payload = make([]byte, req.ContentLength)
		req.Body.Read(pdu.payload)
	}

	// TODO:
	err = pdu.EncodeTLV(METHOD, req.Method)
	err = pdu.EncodeTLV(URL, req.URL.String())
	err = pdu.EncodeTLV(PAYLOAD, pdu.payload)
	// etc ...

	return pdu.Buffer.Bytes(), err
}

func FastDecodeRequest(buf []byte) (*http.Request, error) {
	Log.Traceln("nycu-ucr/net/http2/onvm_encode_decode, FastDecodeRequest")
	req := new(http.Request)
	pdu := &OnvmPDU{
		Buffer: bytes.NewBuffer(buf),
	}

	// TODO:
	tlv1, err := pdu.DecodeTLV() // METHOD
	req.Method = tlv1.Value.(string)
	if err != nil {
		return nil, err
	}

	tlv2, err := pdu.DecodeTLV() // URL
	req.URL, err = url.Parse(tlv2.Value.(string))
	if err != nil {
		return nil, err
	}

	tlv3, err := pdu.DecodeTLV() // PAYLOAD
	if err != nil {
		return nil, err
	}
	if tlv3.Length != 0 {
		req.Body = io.NopCloser(bytes.NewReader(tlv3.Value.([]byte)))
		req.ContentLength = int64(tlv3.Length)
	} else {
		req.Body = nil
		req.ContentLength = 0
	}
	// etc ...

	return req, nil
}

/*********************************
     Methods of http.Response
*********************************/

func FastEncodeResponse(rsp *http.Response) ([]byte, error) {

	return nil, nil
}

func FastDecodeResponse(buf []byte) (*http.Response, error) {

	return nil, nil
}
