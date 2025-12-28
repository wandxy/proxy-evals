package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

type Broker struct {
	clients    map[chan string]bool
	register   chan chan string
	unregister chan chan string
	broadcast  chan string
	mu         sync.RWMutex
}

func newBroker() *Broker {
	return &Broker{
		clients:    make(map[chan string]bool),
		register:   make(chan chan string),
		unregister: make(chan chan string),
		broadcast:  make(chan string),
	}
}

func (b *Broker) run() {
	for {
		select {
		case client := <-b.register:
			b.mu.Lock()
			b.clients[client] = true
			count := len(b.clients)
			b.mu.Unlock()
			log.Printf("Client connected. Total: %d", count)

		case client := <-b.unregister:
			b.mu.Lock()
			if _, ok := b.clients[client]; ok {
				delete(b.clients, client)
				close(client)
			}
			count := len(b.clients)
			b.mu.Unlock()
			log.Printf("Client disconnected. Total: %d", count)

		case msg := <-b.broadcast:
			b.mu.RLock()
			for client := range b.clients {
				select {
				case client <- msg:
				default:
				}
			}
			b.mu.RUnlock()
		}
	}
}

func handleSSE(broker *Broker, w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	client := make(chan string, 10)
	broker.register <- client

	defer func() {
		broker.unregister <- client
	}()

	notify := r.Context().Done()

	fmt.Fprintf(w, "event: connected\ndata: {\"status\":\"connected\"}\n\n")
	flusher.Flush()

	for {
		select {
		case <-notify:
			return
		case msg, ok := <-client:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		}
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func handleBroadcast(broker *Broker, w http.ResponseWriter, r *http.Request) {
	msg := r.URL.Query().Get("msg")
	if msg == "" {
		msg = fmt.Sprintf("Broadcast at %s", time.Now().Format(time.RFC3339))
	}
	broker.broadcast <- msg
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"sent"}`))
}

const clientHTML = `<!DOCTYPE html>
<html>
<head>
    <title>SSE Test Client</title>
    <style>
        body { font-family: monospace; max-width: 800px; margin: 50px auto; padding: 20px; }
        h1 { color: #333; }
        .controls { margin: 20px 0; }
        input[type="text"] { width: 300px; padding: 8px; font-size: 14px; }
        button { padding: 8px 16px; margin: 0 5px; cursor: pointer; }
        button:disabled { opacity: 0.5; cursor: not-allowed; }
        #log {
            background: #1a1a1a; color: #0f0; padding: 15px;
            height: 400px; overflow-y: auto; border-radius: 4px;
            font-size: 13px; line-height: 1.4;
        }
        .status { padding: 5px 10px; border-radius: 3px; display: inline-block; margin-bottom: 10px; }
        .connected { background: #0a0; color: white; }
        .disconnected { background: #a00; color: white; }
        .info { color: #888; margin-top: 20px; font-size: 12px; }
    </style>
</head>
<body>
    <h1>SSE Test Client</h1>

    <div id="status" class="status disconnected">Disconnected</div>

    <div class="controls">
        <input type="text" id="sseUrl" placeholder="SSE URL">
        <button id="connectBtn" onclick="connect()">Connect</button>
        <button id="disconnectBtn" onclick="disconnect()" disabled>Disconnect</button>
    </div>

    <div class="controls">
        <input type="text" id="message" placeholder="Broadcast message" value="Hello from SSE!">
        <button id="broadcastBtn" onclick="broadcast()">Broadcast</button>
    </div>

    <div id="log"></div>

    <div class="info">
        <p>This client tests Server-Sent Events (SSE) streaming through the proxy.</p>
        <p>• <b>Connect</b>: Opens an SSE stream from the server</p>
        <p>• <b>Broadcast</b>: Sends a message to all connected clients via HTTP POST</p>
        <p>• Events should appear in real-time if streaming works correctly</p>
    </div>

    <script>
        let eventSource = null;
        const logEl = document.getElementById('log');
        const statusEl = document.getElementById('status');
        const sseUrlEl = document.getElementById('sseUrl');

        const protocol = window.location.protocol;
        sseUrlEl.value = protocol + '//' + window.location.host + '/events';

        function log(msg, type = 'info') {
            const time = new Date().toLocaleTimeString();
            const colors = { info: '#0f0', error: '#f00', event: '#0ff', system: '#ff0' };
            logEl.innerHTML += '<div style="color:' + (colors[type] || '#0f0') + '">[' + time + '] ' + msg + '</div>';
            logEl.scrollTop = logEl.scrollHeight;
        }

        function updateUI(connected) {
            document.getElementById('connectBtn').disabled = connected;
            document.getElementById('disconnectBtn').disabled = !connected;
            statusEl.className = 'status ' + (connected ? 'connected' : 'disconnected');
            statusEl.textContent = connected ? 'Connected' : 'Disconnected';
        }

        function connect() {
            const url = sseUrlEl.value;
            log('Connecting to ' + url + '...', 'system');

            try {
                eventSource = new EventSource(url);

                eventSource.onopen = function() {
                    log('Connection opened', 'system');
                    updateUI(true);
                };

                eventSource.onmessage = function(e) {
                    log('← ' + e.data, 'event');
                };

                eventSource.addEventListener('connected', function(e) {
                    log('← [connected] ' + e.data, 'event');
                });

                eventSource.onerror = function(e) {
                    log('Error or connection closed', 'error');
                    if (eventSource.readyState === EventSource.CLOSED) {
                        updateUI(false);
                        eventSource = null;
                    }
                };
            } catch (e) {
                log('Failed to connect: ' + e.message, 'error');
            }
        }

        function disconnect() {
            if (eventSource) {
                eventSource.close();
                eventSource = null;
                log('Disconnected', 'system');
                updateUI(false);
            }
        }

        function broadcast() {
            const msg = encodeURIComponent(document.getElementById('message').value);
            fetch('/broadcast?msg=' + msg)
                .then(r => r.json())
                .then(data => log('Broadcast sent', 'system'))
                .catch(e => log('Broadcast failed: ' + e.message, 'error'));
        }
    </script>
</body>
</html>`

func main() {
	addr := flag.String("addr", ":8081", "HTTP service address")
	tlsCert := flag.String("cert", "", "TLS certificate file")
	tlsKey := flag.String("key", "", "TLS key file")
	autoTick := flag.Duration("tick", 0, "Auto-broadcast interval (e.g., 5s)")
	flag.Parse()

	broker := newBroker()
	go broker.run()

	if *autoTick > 0 {
		go func() {
			ticker := time.NewTicker(*autoTick)
			for t := range ticker.C {
				broker.broadcast <- fmt.Sprintf("Tick at %s", t.Format(time.RFC3339))
			}
		}()
	}

	http.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		handleSSE(broker, w, r)
	})

	http.HandleFunc("/broadcast", func(w http.ResponseWriter, r *http.Request) {
		handleBroadcast(broker, w, r)
	})

	http.HandleFunc("/health", handleHealth)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(clientHTML))
	})

	if *tlsCert != "" && *tlsKey != "" {
		log.Printf("Starting SSE server (HTTPS) on %s", *addr)
		log.Fatal(http.ListenAndServeTLS(*addr, *tlsCert, *tlsKey, nil))
	} else {
		log.Printf("Starting SSE server on %s", *addr)
		log.Fatal(http.ListenAndServe(*addr, nil))
	}
}
