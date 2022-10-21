s="./http2/h2c/h2c.go ./http2/h2c/h2c.go ./http2/h2c/h2c.go ./http2/h2c/h2c_test.go ./http2/transport.go ./http2/http2.gopackage ./http2/frame_test.go ./http2/http2_test.go ./http2/README ./http2/transport_test.go ./http2/h2i/h2i.go ./http2/h2i/h2i.go ./http2/h2i/README.md ./http2/server_test.go ./http2/frame.go ./http2/server.go ./http2/write.go"
for f in $s
do
    sed -i 's/golang.org\/x\/net/github.com\/nycu-ucr\/net/g' $f
done
