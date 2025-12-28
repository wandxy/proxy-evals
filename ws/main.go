package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type Hub struct {
	clients    map[*websocket.Conn]bool
	broadcast  chan []byte
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	mu         sync.RWMutex
}

func newHub() *Hub {
	return &Hub{
		clients:    make(map[*websocket.Conn]bool),
		broadcast:  make(chan []byte),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
	}
}

func (h *Hub) run() {
	for {
		select {
		case conn := <-h.register:
			h.mu.Lock()
			h.clients[conn] = true
			count := len(h.clients)
			h.mu.Unlock()
			log.Printf("Client connected. Total: %d", count)

		case conn := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[conn]; ok {
				delete(h.clients, conn)
				conn.Close()
			}
			count := len(h.clients)
			h.mu.Unlock()
			log.Printf("Client disconnected. Total: %d", count)

		case message := <-h.broadcast:
			h.mu.RLock()
			for conn := range h.clients {
				err := conn.WriteMessage(websocket.TextMessage, message)
				if err != nil {
					log.Printf("Broadcast error: %v", err)
				}
			}
			h.mu.RUnlock()
		}
	}
}

func handleWebSocket(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Upgrade error: %v", err)
		return
	}

	hub.register <- conn

	defer func() {
		hub.unregister <- conn
	}()

	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("Read error: %v", err)
			}
			break
		}

		log.Printf("Received: %s", message)

		if messageType == websocket.TextMessage {
			if string(message) == "broadcast" {
				hub.broadcast <- []byte(fmt.Sprintf("Broadcast from server at %s", r.RemoteAddr))
			} else {
				err = conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Echo: %s", message)))
				if err != nil {
					log.Printf("Write error: %v", err)
					break
				}
			}
		}
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

const clientHTML = `<!DOCTYPE html>
<html>
<head>
    <title>WebSocket Test Client</title>
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
    <h1>WebSocket Test Client</h1>

    <div id="status" class="status disconnected">Disconnected</div>

    <div class="controls">
        <input type="text" id="wsUrl" placeholder="WebSocket URL">
        <button id="connectBtn" onclick="connect()">Connect</button>
        <button id="disconnectBtn" onclick="disconnect()" disabled>Disconnect</button>
    </div>

    <div class="controls">
        <input type="text" id="message" placeholder="Message to send" onkeypress="if(event.key==='Enter')send()">
        <button id="sendBtn" onclick="send()" disabled>Send</button>
        <button id="broadcastBtn" onclick="broadcast()" disabled>Broadcast</button>
    </div>

    <div id="log"></div>

    <div class="info">
        <p>This client tests WebSocket connectivity through the proxy.</p>
        <p>• <b>Send</b>: Echoes your message back</p>
        <p>• <b>Broadcast</b>: Sends message to all connected clients</p>
    </div>

    <script>
        let ws = null;
        const logEl = document.getElementById('log');
        const statusEl = document.getElementById('status');
        const wsUrlEl = document.getElementById('wsUrl');

        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        wsUrlEl.value = protocol + '//' + window.location.host + '/ws';

        function log(msg, type = 'info') {
            const time = new Date().toLocaleTimeString();
            const colors = { info: '#0f0', error: '#f00', sent: '#ff0', recv: '#0ff' };
            logEl.innerHTML += '<div style="color:' + (colors[type] || '#0f0') + '">[' + time + '] ' + msg + '</div>';
            logEl.scrollTop = logEl.scrollHeight;
        }

        function updateUI(connected) {
            document.getElementById('connectBtn').disabled = connected;
            document.getElementById('disconnectBtn').disabled = !connected;
            document.getElementById('sendBtn').disabled = !connected;
            document.getElementById('broadcastBtn').disabled = !connected;
            statusEl.className = 'status ' + (connected ? 'connected' : 'disconnected');
            statusEl.textContent = connected ? 'Connected' : 'Disconnected';
        }

        function connect() {
            const url = wsUrlEl.value;
            log('Connecting to ' + url + '...');

            try {
                ws = new WebSocket(url);

                ws.onopen = function() {
                    log('Connected!');
                    updateUI(true);
                };

                ws.onmessage = function(e) {
                    log('← ' + e.data, 'recv');
                };

                ws.onerror = function(e) {
                    log('Error: ' + (e.message || 'Connection error'), 'error');
                };

                ws.onclose = function(e) {
                    log('Disconnected (code: ' + e.code + ', reason: ' + (e.reason || 'none') + ')');
                    updateUI(false);
                    ws = null;
                };
            } catch (e) {
                log('Failed to connect: ' + e.message, 'error');
            }
        }

        function disconnect() {
            if (ws) {
                ws.close();
            }
        }

        function send() {
            const msg = document.getElementById('message').value;
            if (ws && msg) {
                ws.send(msg);
                log('→ ' + msg, 'sent');
                document.getElementById('message').value = '';
            }
        }

        function broadcast() {
            if (ws) {
                ws.send('broadcast');
                log('→ broadcast', 'sent');
            }
        }
    </script>
</body>
</html>`

func main() {
	addr := flag.String("addr", ":8080", "HTTP service address")
	tlsCert := flag.String("cert", "", "TLS certificate file (enables HTTPS/WSS)")
	tlsKey := flag.String("key", "", "TLS key file")
	flag.Parse()

	hub := newHub()
	go hub.run()

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		handleWebSocket(hub, w, r)
	})

	http.HandleFunc("/health", handleHealth)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(clientHTML))
	})

	if *tlsCert != "" && *tlsKey != "" {
		log.Printf("Starting WSS server on %s", *addr)
		log.Fatal(http.ListenAndServeTLS(*addr, *tlsCert, *tlsKey, nil))
	} else {
		log.Printf("Starting WS server on %s", *addr)
		log.Fatal(http.ListenAndServe(*addr, nil))
	}
}
