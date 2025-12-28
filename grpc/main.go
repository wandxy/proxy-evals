package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
)

type EchoServer struct {
	UnimplementedEchoServiceServer
}

func (s *EchoServer) Echo(ctx context.Context, req *EchoRequest) (*EchoResponse, error) {
	log.Printf("Echo request: %s", req.Message)
	return &EchoResponse{
		Message:   req.Message,
		Timestamp: time.Now().Unix(),
	}, nil
}

func (s *EchoServer) ServerStream(req *StreamRequest, stream EchoService_ServerStreamServer) error {
	log.Printf("ServerStream request: count=%d", req.Count)

	for i := int32(0); i < req.Count; i++ {
		if err := stream.Send(&StreamResponse{
			Index:     i,
			Message:   fmt.Sprintf("Message %d of %d", i+1, req.Count),
			Timestamp: time.Now().Unix(),
		}); err != nil {
			return err
		}
		time.Sleep(time.Duration(req.DelayMs) * time.Millisecond)
	}

	return nil
}

func (s *EchoServer) ClientStream(stream EchoService_ClientStreamServer) error {
	var count int32
	var messages []string

	for {
		req, err := stream.Recv()
		if err == io.EOF {
			log.Printf("ClientStream completed: received %d messages", count)
			return stream.SendAndClose(&ClientStreamResponse{
				Count:    count,
				Messages: messages,
			})
		}
		if err != nil {
			return err
		}

		count++
		messages = append(messages, req.Message)
		log.Printf("ClientStream received: %s", req.Message)
	}
}

func (s *EchoServer) BidirectionalStream(stream EchoService_BidirectionalStreamServer) error {
	log.Printf("BidirectionalStream started")

	for {
		req, err := stream.Recv()
		if err == io.EOF {
			log.Printf("BidirectionalStream completed")
			return nil
		}
		if err != nil {
			return err
		}

		log.Printf("BidirectionalStream received: %s", req.Message)

		resp := &StreamResponse{
			Index:     0,
			Message:   "Echo: " + req.Message,
			Timestamp: time.Now().Unix(),
		}

		if err := stream.Send(resp); err != nil {
			return err
		}
	}
}

type HealthServer struct {
	UnimplementedHealthServiceServer
}

func (s *HealthServer) Check(ctx context.Context, req *HealthCheckRequest) (*HealthCheckResponse, error) {
	return &HealthCheckResponse{
		Status: "SERVING",
	}, nil
}

const clientHTML = `<!DOCTYPE html>
<html>
<head>
    <title>gRPC Test Client</title>
    <style>
        body { font-family: monospace; max-width: 900px; margin: 50px auto; padding: 20px; }
        h1 { color: #333; }
        .test-section { margin: 30px 0; padding: 20px; border: 1px solid #ddd; border-radius: 8px; }
        .test-section h2 { margin-top: 0; color: #555; }
        label { display: inline-block; width: 120px; }
        input[type="text"], input[type="number"] { padding: 5px; margin: 5px 0; }
        input[type="text"] { width: 300px; }
        input[type="number"] { width: 80px; }
        button { padding: 10px 20px; margin: 10px 5px 10px 0; cursor: pointer; }
        button:disabled { opacity: 0.5; cursor: not-allowed; }
        .result { background: #f5f5f5; padding: 15px; margin-top: 10px; border-radius: 4px;
                  font-size: 13px; white-space: pre-wrap; max-height: 200px; overflow-y: auto; }
        #log { background: #1a1a1a; color: #0f0; padding: 15px; height: 200px; overflow-y: auto;
               border-radius: 4px; font-size: 13px; }
        .info { color: #888; margin-top: 20px; font-size: 12px; }
        .note { background: #fff3cd; padding: 10px; border-radius: 4px; margin: 10px 0; }
    </style>
</head>
<body>
    <h1>gRPC Test Client</h1>

    <div class="note">
        <strong>Note:</strong> This is a web client. gRPC-Web requires a proxy (like Envoy) to translate HTTP/1.1 to gRPC.
        For full gRPC testing, use a native gRPC client (grpcurl, Postman, or custom code).
    </div>

    <div class="test-section">
        <h2>Unary RPC (Echo)</h2>
        <p>Single request-response pattern.</p>
        <div>
            <label>Message:</label>
            <input type="text" id="echoMessage" value="Hello gRPC" placeholder="Enter message...">
        </div>
        <button onclick="testEcho()">Call Echo</button>
        <div class="result" id="echoResult"></div>
    </div>

    <div class="test-section">
        <h2>Server Streaming RPC</h2>
        <p>Single request, multiple responses streamed from server.</p>
        <div>
            <label>Count:</label>
            <input type="number" id="streamCount" value="5" min="1" max="20">
            <label>Delay (ms):</label>
            <input type="number" id="streamDelay" value="500" min="0">
        </div>
        <button onclick="testServerStream()">Start Server Stream</button>
        <div class="result" id="streamResult"></div>
    </div>

    <div id="log"></div>

    <div class="info">
        <p>gRPC streaming types:</p>
        <ul>
            <li><b>Unary</b>: Single request → single response (like REST)</li>
            <li><b>Server streaming</b>: Single request → stream of responses</li>
            <li><b>Client streaming</b>: Stream of requests → single response</li>
            <li><b>Bidirectional streaming</b>: Stream of requests ↔ stream of responses</li>
        </ul>
        <p>Testing gRPC through HTTP proxies:</p>
        <ul>
            <li>gRPC uses HTTP/2 as transport</li>
            <li>Standard gRPC requires HTTP/2 and binary protobuf</li>
            <li>Web browsers need gRPC-Web with a translation proxy</li>
        </ul>
        <p><strong>To test this server:</strong> Use <code>grpcurl</code> or similar gRPC client tools.</p>
        <pre style="background: #f5f5f5; padding: 10px; border-radius: 4px;">
# List services
grpcurl -plaintext localhost:50051 list

# Call Echo
grpcurl -plaintext -d '{"message":"hello"}' localhost:50051 EchoService/Echo

# Server stream
grpcurl -plaintext -d '{"count":5,"delay_ms":500}' localhost:50051 EchoService/ServerStream
        </pre>
    </div>

    <script>
        const logEl = document.getElementById('log');

        function log(msg, type = 'info') {
            const time = new Date().toLocaleTimeString();
            const colors = { info: '#0f0', error: '#f00', success: '#0ff', warn: '#ff0' };
            logEl.innerHTML += '<div style="color:' + (colors[type] || '#0f0') + '">[' + time + '] ' + msg + '</div>';
            logEl.scrollTop = logEl.scrollHeight;
        }

        function testEcho() {
            document.getElementById('echoResult').textContent = 'This requires a gRPC client. Use grpcurl or similar tools.';
            log('Use: grpcurl -plaintext -d \'{"message":"hello"}\' host:port EchoService/Echo', 'info');
        }

        function testServerStream() {
            document.getElementById('streamResult').textContent = 'This requires a gRPC client. Use grpcurl or similar tools.';
            log('Use: grpcurl -plaintext -d \'{"count":5,"delay_ms":500}\' host:port EchoService/ServerStream', 'info');
        }

        log('gRPC server is running. Use grpcurl or native gRPC clients to test.');
        log('Example: grpcurl -plaintext ' + window.location.hostname + ':50051 list', 'success');
    </script>
</body>
</html>`

func main() {
	grpcPort := flag.String("grpc-port", "50051", "gRPC server port")
	httpPort := flag.String("http-port", "8080", "HTTP info page port")
	flag.Parse()

	lis, err := net.Listen("tcp", ":"+*grpcPort)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	RegisterEchoServiceServer(grpcServer, &EchoServer{})
	RegisterHealthServiceServer(grpcServer, &HealthServer{})

	reflection.Register(grpcServer)

	go func() {
		log.Printf("Starting gRPC server on :%s", *grpcPort)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("Failed to serve: %v", err)
		}
	}()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(clientHTML))
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		conn, err := grpc.Dial("localhost:"+*grpcPort, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			http.Error(w, "gRPC server unavailable", http.StatusServiceUnavailable)
			return
		}
		defer conn.Close()

		client := NewHealthServiceClient(conn)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		resp, err := client.Check(ctx, &HealthCheckRequest{})
		if err != nil {
			http.Error(w, "Health check failed", http.StatusServiceUnavailable)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fmt.Sprintf(`{"status":"%s"}`, resp.Status)))
	})

	log.Printf("Starting HTTP info server on :%s", *httpPort)
	log.Fatal(http.ListenAndServe(":"+*httpPort, nil))
}
