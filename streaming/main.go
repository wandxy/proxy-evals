package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"time"
)

func handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	sizeStr := r.URL.Query().Get("size")
	size := 1024 * 1024
	if sizeStr != "" {
		if s, err := strconv.Atoi(sizeStr); err == nil && s > 0 {
			size = s
		}
	}

	delayStr := r.URL.Query().Get("delay")
	delay := 0
	if delayStr != "" {
		if d, err := strconv.Atoi(delayStr); err == nil && d > 0 {
			delay = d
		}
	}

	chunkStr := r.URL.Query().Get("chunk")
	chunkSize := 8192
	if chunkStr != "" {
		if c, err := strconv.Atoi(chunkStr); err == nil && c > 0 {
			chunkSize = c
		}
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Size", strconv.Itoa(size))
	w.Header().Set("X-Chunk-Size", strconv.Itoa(chunkSize))

	log.Printf("Starting stream: size=%d, chunk=%d, delay=%dms", size, chunkSize, delay)

	sent := 0
	chunk := make([]byte, chunkSize)
	for i := range chunk {
		chunk[i] = byte(rand.Intn(256))
	}

	for sent < size {
		remaining := size - sent
		toSend := chunkSize
		if remaining < chunkSize {
			toSend = remaining
		}

		n, err := w.Write(chunk[:toSend])
		if err != nil {
			log.Printf("Stream write error after %d bytes: %v", sent, err)
			return
		}
		sent += n
		flusher.Flush()

		if delay > 0 {
			time.Sleep(time.Duration(delay) * time.Millisecond)
		}
	}

	log.Printf("Stream complete: sent %d bytes", sent)
}

func handleChunked(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	countStr := r.URL.Query().Get("count")
	count := 10
	if countStr != "" {
		if c, err := strconv.Atoi(countStr); err == nil && c > 0 {
			count = c
		}
	}

	delayStr := r.URL.Query().Get("delay")
	delay := 500
	if delayStr != "" {
		if d, err := strconv.Atoi(delayStr); err == nil && d > 0 {
			delay = d
		}
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Transfer-Encoding", "chunked")

	log.Printf("Starting chunked response: count=%d, delay=%dms", count, delay)

	for i := 1; i <= count; i++ {
		msg := fmt.Sprintf("Chunk %d of %d at %s\n", i, count, time.Now().Format(time.RFC3339Nano))
		w.Write([]byte(msg))
		flusher.Flush()

		if i < count {
			time.Sleep(time.Duration(delay) * time.Millisecond)
		}
	}

	log.Printf("Chunked response complete: sent %d chunks", count)
}

func handleSlowHeaders(w http.ResponseWriter, r *http.Request) {
	delayStr := r.URL.Query().Get("delay")
	delay := 2000
	if delayStr != "" {
		if d, err := strconv.Atoi(delayStr); err == nil && d > 0 {
			delay = d
		}
	}

	log.Printf("Slow headers: delaying %dms before sending response", delay)
	time.Sleep(time.Duration(delay) * time.Millisecond)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok","delayed":true}`))
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

const clientHTML = `<!DOCTYPE html>
<html>
<head>
    <title>Streaming Test Client</title>
    <style>
        body { font-family: monospace; max-width: 900px; margin: 50px auto; padding: 20px; }
        h1 { color: #333; }
        .test-section { margin: 30px 0; padding: 20px; border: 1px solid #ddd; border-radius: 8px; }
        .test-section h2 { margin-top: 0; color: #555; }
        label { display: inline-block; width: 120px; }
        input[type="number"] { width: 100px; padding: 5px; }
        button { padding: 10px 20px; margin: 10px 5px 10px 0; cursor: pointer; }
        button:disabled { opacity: 0.5; cursor: not-allowed; }
        .progress { margin: 10px 0; }
        .progress-bar { height: 20px; background: #eee; border-radius: 4px; overflow: hidden; }
        .progress-fill { height: 100%; background: #4caf50; transition: width 0.1s; }
        .stats { font-size: 14px; color: #666; }
        #log { background: #1a1a1a; color: #0f0; padding: 15px; height: 200px; overflow-y: auto;
               border-radius: 4px; font-size: 13px; }
        .info { color: #888; margin-top: 20px; font-size: 12px; }
    </style>
</head>
<body>
    <h1>Streaming Test Client</h1>

    <div class="test-section">
        <h2>Binary Stream Test</h2>
        <p>Downloads binary data in chunks with optional delay between chunks.</p>
        <div>
            <label>Size (bytes):</label>
            <input type="number" id="streamSize" value="1048576" min="1024">
            <label>Chunk:</label>
            <input type="number" id="streamChunk" value="8192" min="1024">
            <label>Delay (ms):</label>
            <input type="number" id="streamDelay" value="0" min="0">
        </div>
        <button id="streamBtn" onclick="testStream()">Start Stream</button>
        <button onclick="abortStream()" id="abortStreamBtn" disabled>Abort</button>
        <div class="progress">
            <div class="progress-bar"><div class="progress-fill" id="streamProgress" style="width:0%"></div></div>
        </div>
        <div class="stats" id="streamStats"></div>
    </div>

    <div class="test-section">
        <h2>Chunked Transfer Test</h2>
        <p>Receives text chunks with delays to test chunked transfer encoding.</p>
        <div>
            <label>Chunks:</label>
            <input type="number" id="chunkedCount" value="10" min="1">
            <label>Delay (ms):</label>
            <input type="number" id="chunkedDelay" value="500" min="0">
        </div>
        <button id="chunkedBtn" onclick="testChunked()">Start Chunked</button>
        <div class="stats" id="chunkedStats"></div>
    </div>

    <div class="test-section">
        <h2>Slow Headers Test</h2>
        <p>Tests proxy timeout handling by delaying the response headers.</p>
        <div>
            <label>Delay (ms):</label>
            <input type="number" id="slowDelay" value="2000" min="100">
        </div>
        <button id="slowBtn" onclick="testSlow()">Test Slow Headers</button>
        <div class="stats" id="slowStats"></div>
    </div>

    <div id="log"></div>

    <div class="info">
        <p>This client tests various streaming scenarios through the proxy:</p>
        <ul>
            <li><b>Binary Stream</b>: Large file downloads with progress tracking</li>
            <li><b>Chunked Transfer</b>: HTTP chunked encoding with visible delays</li>
            <li><b>Slow Headers</b>: Delayed response to test timeout handling</li>
        </ul>
    </div>

    <script>
        let streamController = null;
        const logEl = document.getElementById('log');

        function log(msg, type = 'info') {
            const time = new Date().toLocaleTimeString();
            const colors = { info: '#0f0', error: '#f00', success: '#0ff', warn: '#ff0' };
            logEl.innerHTML += '<div style="color:' + (colors[type] || '#0f0') + '">[' + time + '] ' + msg + '</div>';
            logEl.scrollTop = logEl.scrollHeight;
        }

        function formatBytes(bytes) {
            if (bytes < 1024) return bytes + ' B';
            if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(2) + ' KB';
            return (bytes / (1024 * 1024)).toFixed(2) + ' MB';
        }

        async function testStream() {
            const size = document.getElementById('streamSize').value;
            const chunk = document.getElementById('streamChunk').value;
            const delay = document.getElementById('streamDelay').value;

            document.getElementById('streamBtn').disabled = true;
            document.getElementById('abortStreamBtn').disabled = false;
            document.getElementById('streamProgress').style.width = '0%';

            streamController = new AbortController();
            const startTime = Date.now();
            let received = 0;

            log('Starting binary stream: ' + formatBytes(parseInt(size)));

            try {
                const response = await fetch('/stream?size=' + size + '&chunk=' + chunk + '&delay=' + delay, {
                    signal: streamController.signal
                });

                const reader = response.body.getReader();
                const totalSize = parseInt(size);

                while (true) {
                    const { done, value } = await reader.read();
                    if (done) break;

                    received += value.length;
                    const percent = Math.min(100, (received / totalSize) * 100);
                    document.getElementById('streamProgress').style.width = percent + '%';

                    const elapsed = (Date.now() - startTime) / 1000;
                    const speed = received / elapsed;
                    document.getElementById('streamStats').textContent =
                        formatBytes(received) + ' / ' + formatBytes(totalSize) +
                        ' (' + percent.toFixed(1) + '%) - ' + formatBytes(speed) + '/s';
                }

                const elapsed = (Date.now() - startTime) / 1000;
                log('Stream complete: ' + formatBytes(received) + ' in ' + elapsed.toFixed(2) + 's', 'success');
            } catch (e) {
                if (e.name === 'AbortError') {
                    log('Stream aborted by user', 'warn');
                } else {
                    log('Stream error: ' + e.message, 'error');
                }
            } finally {
                document.getElementById('streamBtn').disabled = false;
                document.getElementById('abortStreamBtn').disabled = true;
                streamController = null;
            }
        }

        function abortStream() {
            if (streamController) {
                streamController.abort();
            }
        }

        async function testChunked() {
            const count = document.getElementById('chunkedCount').value;
            const delay = document.getElementById('chunkedDelay').value;

            document.getElementById('chunkedBtn').disabled = true;
            log('Starting chunked transfer: ' + count + ' chunks, ' + delay + 'ms delay');

            const startTime = Date.now();
            let chunks = 0;

            try {
                const response = await fetch('/chunked?count=' + count + '&delay=' + delay);
                const reader = response.body.getReader();
                const decoder = new TextDecoder();

                while (true) {
                    const { done, value } = await reader.read();
                    if (done) break;

                    const text = decoder.decode(value, { stream: true });
                    chunks++;
                    document.getElementById('chunkedStats').textContent =
                        'Received chunk ' + chunks + ': ' + text.trim();
                    log('Chunk ' + chunks + ': ' + text.trim());
                }

                const elapsed = (Date.now() - startTime) / 1000;
                log('Chunked transfer complete: ' + chunks + ' chunks in ' + elapsed.toFixed(2) + 's', 'success');
            } catch (e) {
                log('Chunked error: ' + e.message, 'error');
            } finally {
                document.getElementById('chunkedBtn').disabled = false;
            }
        }

        async function testSlow() {
            const delay = document.getElementById('slowDelay').value;

            document.getElementById('slowBtn').disabled = true;
            document.getElementById('slowStats').textContent = 'Waiting for response...';
            log('Testing slow headers: ' + delay + 'ms delay');

            const startTime = Date.now();

            try {
                const response = await fetch('/slow?delay=' + delay);
                const data = await response.json();
                const elapsed = Date.now() - startTime;

                document.getElementById('slowStats').textContent =
                    'Response received in ' + elapsed + 'ms';
                log('Slow headers response: ' + JSON.stringify(data) + ' in ' + elapsed + 'ms', 'success');
            } catch (e) {
                log('Slow headers error: ' + e.message, 'error');
                document.getElementById('slowStats').textContent = 'Error: ' + e.message;
            } finally {
                document.getElementById('slowBtn').disabled = false;
            }
        }
    </script>
</body>
</html>`

func main() {
	addr := flag.String("addr", ":8080", "HTTP service address")
	tlsCert := flag.String("cert", "", "TLS certificate file (enables HTTPS)")
	tlsKey := flag.String("key", "", "TLS key file")
	flag.Parse()

	http.HandleFunc("/stream", handleStream)
	http.HandleFunc("/chunked", handleChunked)
	http.HandleFunc("/slow", handleSlowHeaders)
	http.HandleFunc("/health", handleHealth)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(clientHTML))
	})

	if *tlsCert != "" && *tlsKey != "" {
		log.Printf("Starting HTTPS streaming server on %s", *addr)
		log.Fatal(http.ListenAndServeTLS(*addr, *tlsCert, *tlsKey, nil))
	} else {
		log.Printf("Starting HTTP streaming server on %s", *addr)
		log.Fatal(http.ListenAndServe(*addr, nil))
	}
}
