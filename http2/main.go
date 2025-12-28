package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

func handleInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	proto := r.Proto
	tlsInfo := "none"
	if r.TLS != nil {
		tlsInfo = fmt.Sprintf("version=%d, cipher=%d", r.TLS.Version, r.TLS.CipherSuite)
	}

	json := fmt.Sprintf(`{
  "protocol": %q,
  "method": %q,
  "url": %q,
  "host": %q,
  "remote_addr": %q,
  "tls": %q,
  "headers": {`, proto, r.Method, r.URL.String(), r.Host, r.RemoteAddr, tlsInfo)

	first := true
	for k, v := range r.Header {
		if !first {
			json += ","
		}
		json += fmt.Sprintf("\n    %q: %q", k, strings.Join(v, ", "))
		first = false
	}
	json += "\n  }\n}"

	w.Write([]byte(json))
	log.Printf("Info request: proto=%s, method=%s, url=%s", proto, r.Method, r.URL.String())
}

func handlePush(w http.ResponseWriter, r *http.Request) {
	pusher, ok := w.(http.Pusher)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"push_supported": false, "message": "Server push not available (HTTP/1.1 or push disabled)"}`))
		log.Printf("Push not supported for %s", r.Proto)
		return
	}

	resources := []string{"/pushed-resource-1", "/pushed-resource-2", "/pushed-resource-3"}

	pushed := []string{}
	for _, res := range resources {
		err := pusher.Push(res, nil)
		if err != nil {
			log.Printf("Push failed for %s: %v", res, err)
		} else {
			pushed = append(pushed, res)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json := fmt.Sprintf(`{"push_supported": true, "pushed": ["%s"]}`, strings.Join(pushed, `", "`))
	w.Write([]byte(json))
	log.Printf("Pushed %d resources", len(pushed))
}

func handlePushedResource(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "max-age=3600")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf(`{"resource": %q, "timestamp": %q}`, r.URL.Path, time.Now().Format(time.RFC3339))))
}

func handleMultiplex(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	countStr := r.URL.Query().Get("count")
	count := 5
	if countStr != "" {
		if c, err := strconv.Atoi(countStr); err == nil && c > 0 && c <= 20 {
			count = c
		}
	}

	delayStr := r.URL.Query().Get("delay")
	delay := 200
	if delayStr != "" {
		if d, err := strconv.Atoi(delayStr); err == nil && d >= 0 {
			delay = d
		}
	}

	w.Header().Set("Content-Type", "text/plain")

	log.Printf("Multiplex test: count=%d, delay=%dms, proto=%s", count, delay, r.Proto)

	for i := 1; i <= count; i++ {
		msg := fmt.Sprintf("Message %d/%d at %s (proto: %s)\n", i, count, time.Now().Format(time.RFC3339Nano), r.Proto)
		w.Write([]byte(msg))
		flusher.Flush()

		if i < count && delay > 0 {
			time.Sleep(time.Duration(delay) * time.Millisecond)
		}
	}
}

func handleConcurrent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	delayStr := r.URL.Query().Get("delay")
	delay := 100
	if delayStr != "" {
		if d, err := strconv.Atoi(delayStr); err == nil && d >= 0 {
			delay = d
		}
	}

	time.Sleep(time.Duration(delay) * time.Millisecond)

	json := fmt.Sprintf(`{"request_id": %q, "delay_ms": %d, "protocol": %q, "timestamp": %q}`,
		r.URL.Query().Get("id"), delay, r.Proto, time.Now().Format(time.RFC3339Nano))
	w.Write([]byte(json))
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

const clientHTML = `<!DOCTYPE html>
<html>
<head>
    <title>HTTP/2 Test Client</title>
    <style>
        body { font-family: monospace; max-width: 900px; margin: 50px auto; padding: 20px; }
        h1 { color: #333; }
        .test-section { margin: 30px 0; padding: 20px; border: 1px solid #ddd; border-radius: 8px; }
        .test-section h2 { margin-top: 0; color: #555; }
        label { display: inline-block; width: 100px; }
        input[type="number"] { width: 80px; padding: 5px; }
        button { padding: 10px 20px; margin: 10px 5px 10px 0; cursor: pointer; }
        button:disabled { opacity: 0.5; cursor: not-allowed; }
        .result { background: #f5f5f5; padding: 15px; margin-top: 10px; border-radius: 4px;
                  font-size: 13px; white-space: pre-wrap; max-height: 200px; overflow-y: auto; }
        #log { background: #1a1a1a; color: #0f0; padding: 15px; height: 250px; overflow-y: auto;
               border-radius: 4px; font-size: 13px; }
        .protocol-badge { display: inline-block; padding: 3px 8px; border-radius: 3px;
                         font-size: 12px; margin-left: 10px; }
        .h2 { background: #4caf50; color: white; }
        .h1 { background: #ff9800; color: white; }
        .info { color: #888; margin-top: 20px; font-size: 12px; }
    </style>
</head>
<body>
    <h1>HTTP/2 Test Client <span id="protoBadge" class="protocol-badge">Detecting...</span></h1>

    <div class="test-section">
        <h2>Connection Info</h2>
        <p>Shows the protocol negotiated for this connection.</p>
        <button onclick="testInfo()">Get Connection Info</button>
        <div class="result" id="infoResult"></div>
    </div>

    <div class="test-section">
        <h2>Server Push Test</h2>
        <p>Tests HTTP/2 server push (only works with HTTP/2 + TLS).</p>
        <button onclick="testPush()">Test Server Push</button>
        <div class="result" id="pushResult"></div>
    </div>

    <div class="test-section">
        <h2>Multiplexing Test</h2>
        <p>Streams multiple messages over a single connection.</p>
        <div>
            <label>Messages:</label>
            <input type="number" id="muxCount" value="5" min="1" max="20">
            <label>Delay (ms):</label>
            <input type="number" id="muxDelay" value="200" min="0">
        </div>
        <button onclick="testMultiplex()">Test Multiplexing</button>
        <div class="result" id="muxResult"></div>
    </div>

    <div class="test-section">
        <h2>Concurrent Requests Test</h2>
        <p>Sends multiple requests simultaneously to test HTTP/2 multiplexing.</p>
        <div>
            <label>Requests:</label>
            <input type="number" id="concurrentCount" value="10" min="1" max="50">
            <label>Delay (ms):</label>
            <input type="number" id="concurrentDelay" value="100" min="0">
        </div>
        <button onclick="testConcurrent()">Test Concurrent</button>
        <div class="result" id="concurrentResult"></div>
    </div>

    <div id="log"></div>

    <div class="info">
        <p>HTTP/2 features being tested:</p>
        <ul>
            <li><b>Connection Info</b>: Verify protocol negotiation (h2 vs http/1.1)</li>
            <li><b>Server Push</b>: HTTP/2 push promises (requires TLS)</li>
            <li><b>Multiplexing</b>: Multiple frames over single connection</li>
            <li><b>Concurrent Requests</b>: Parallel requests without head-of-line blocking</li>
        </ul>
    </div>

    <script>
        const logEl = document.getElementById('log');

        function log(msg, type = 'info') {
            const time = new Date().toLocaleTimeString();
            const colors = { info: '#0f0', error: '#f00', success: '#0ff', warn: '#ff0' };
            logEl.innerHTML += '<div style="color:' + (colors[type] || '#0f0') + '">[' + time + '] ' + msg + '</div>';
            logEl.scrollTop = logEl.scrollHeight;
        }

        function updateProtoBadge(proto) {
            const badge = document.getElementById('protoBadge');
            if (proto.includes('2')) {
                badge.textContent = 'HTTP/2';
                badge.className = 'protocol-badge h2';
            } else {
                badge.textContent = 'HTTP/1.1';
                badge.className = 'protocol-badge h1';
            }
        }

        async function testInfo() {
            log('Fetching connection info...');
            try {
                const response = await fetch('/info');
                const data = await response.json();
                document.getElementById('infoResult').textContent = JSON.stringify(data, null, 2);
                updateProtoBadge(data.protocol);
                log('Protocol: ' + data.protocol, 'success');
            } catch (e) {
                log('Error: ' + e.message, 'error');
                document.getElementById('infoResult').textContent = 'Error: ' + e.message;
            }
        }

        async function testPush() {
            log('Testing server push...');
            try {
                const response = await fetch('/push');
                const data = await response.json();
                document.getElementById('pushResult').textContent = JSON.stringify(data, null, 2);
                if (data.push_supported) {
                    log('Server push supported, pushed ' + data.pushed.length + ' resources', 'success');
                } else {
                    log('Server push not supported: ' + data.message, 'warn');
                }
            } catch (e) {
                log('Error: ' + e.message, 'error');
                document.getElementById('pushResult').textContent = 'Error: ' + e.message;
            }
        }

        async function testMultiplex() {
            const count = document.getElementById('muxCount').value;
            const delay = document.getElementById('muxDelay').value;

            log('Testing multiplexing: ' + count + ' messages, ' + delay + 'ms delay');
            document.getElementById('muxResult').textContent = '';

            try {
                const response = await fetch('/multiplex?count=' + count + '&delay=' + delay);
                const reader = response.body.getReader();
                const decoder = new TextDecoder();
                let result = '';

                while (true) {
                    const { done, value } = await reader.read();
                    if (done) break;
                    const text = decoder.decode(value, { stream: true });
                    result += text;
                    document.getElementById('muxResult').textContent = result;
                }

                log('Multiplexing complete', 'success');
            } catch (e) {
                log('Error: ' + e.message, 'error');
                document.getElementById('muxResult').textContent = 'Error: ' + e.message;
            }
        }

        async function testConcurrent() {
            const count = parseInt(document.getElementById('concurrentCount').value);
            const delay = document.getElementById('concurrentDelay').value;

            log('Sending ' + count + ' concurrent requests...');
            const startTime = Date.now();

            try {
                const promises = [];
                for (let i = 0; i < count; i++) {
                    promises.push(
                        fetch('/concurrent?id=' + i + '&delay=' + delay)
                            .then(r => r.json())
                            .then(data => ({ id: i, data, time: Date.now() - startTime }))
                    );
                }

                const results = await Promise.all(promises);
                const totalTime = Date.now() - startTime;

                let output = 'Total time: ' + totalTime + 'ms\n';
                output += 'Expected sequential time: ' + (count * parseInt(delay)) + 'ms\n\n';
                results.sort((a, b) => a.time - b.time);
                results.forEach(r => {
                    output += 'Request ' + r.id + ': completed at ' + r.time + 'ms (' + r.data.protocol + ')\n';
                });

                document.getElementById('concurrentResult').textContent = output;

                const speedup = (count * parseInt(delay)) / totalTime;
                log('Concurrent test complete: ' + totalTime + 'ms (speedup: ' + speedup.toFixed(2) + 'x)', 'success');
            } catch (e) {
                log('Error: ' + e.message, 'error');
                document.getElementById('concurrentResult').textContent = 'Error: ' + e.message;
            }
        }

        testInfo();
    </script>
</body>
</html>`

func main() {
	addr := flag.String("addr", ":8080", "HTTP service address")
	tlsCert := flag.String("cert", "", "TLS certificate file (enables HTTPS/H2)")
	tlsKey := flag.String("key", "", "TLS key file")
	h2cEnabled := flag.Bool("h2c", true, "Enable h2c (HTTP/2 cleartext) when not using TLS")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/info", handleInfo)
	mux.HandleFunc("/push", handlePush)
	mux.HandleFunc("/pushed-resource-1", handlePushedResource)
	mux.HandleFunc("/pushed-resource-2", handlePushedResource)
	mux.HandleFunc("/pushed-resource-3", handlePushedResource)
	mux.HandleFunc("/multiplex", handleMultiplex)
	mux.HandleFunc("/concurrent", handleConcurrent)
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(clientHTML))
	})

	if *tlsCert != "" && *tlsKey != "" {
		server := &http.Server{
			Addr:    *addr,
			Handler: mux,
			TLSConfig: &tls.Config{
				NextProtos: []string{"h2", "http/1.1"},
			},
		}
		http2.ConfigureServer(server, &http2.Server{})

		log.Printf("Starting HTTP/2 (h2) server on %s", *addr)
		log.Fatal(server.ListenAndServeTLS(*tlsCert, *tlsKey))
	} else {
		var handler http.Handler = mux
		if *h2cEnabled {
			h2s := &http2.Server{}
			handler = h2c.NewHandler(mux, h2s)
			log.Printf("Starting HTTP/2 (h2c) server on %s", *addr)
		} else {
			log.Printf("Starting HTTP/1.1 server on %s", *addr)
		}
		log.Fatal(http.ListenAndServe(*addr, handler))
	}
}
