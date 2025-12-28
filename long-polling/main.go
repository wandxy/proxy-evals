package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type Message struct {
	ID        int       `json:"id"`
	Text      string    `json:"text"`
	Timestamp time.Time `json:"timestamp"`
}

type MessageBroker struct {
	mu       sync.RWMutex
	messages []Message
	nextID   int
}

func NewMessageBroker() *MessageBroker {
	return &MessageBroker{
		messages: make([]Message, 0),
		nextID:   1,
	}
}

func (mb *MessageBroker) AddMessage(text string) Message {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	msg := Message{
		ID:        mb.nextID,
		Text:      text,
		Timestamp: time.Now(),
	}
	mb.nextID++
	mb.messages = append(mb.messages, msg)

	if len(mb.messages) > 100 {
		mb.messages = mb.messages[len(mb.messages)-100:]
	}

	return msg
}

func (mb *MessageBroker) GetMessagesSince(sinceID int, timeout time.Duration) []Message {
	start := time.Now()
	for {
		mb.mu.RLock()
		var newMessages []Message
		for _, msg := range mb.messages {
			if msg.ID > sinceID {
				newMessages = append(newMessages, msg)
			}
		}
		mb.mu.RUnlock()

		if len(newMessages) > 0 {
			return newMessages
		}

		if time.Since(start) >= timeout {
			return []Message{}
		}

		time.Sleep(100 * time.Millisecond)
	}
}

func (mb *MessageBroker) GetAllMessages() []Message {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	result := make([]Message, len(mb.messages))
	copy(result, mb.messages)
	return result
}

var broker *MessageBroker

func handlePoll(w http.ResponseWriter, r *http.Request) {
	sinceIDStr := r.URL.Query().Get("since")
	sinceID := 0
	if sinceIDStr != "" {
		if id, err := strconv.Atoi(sinceIDStr); err == nil {
			sinceID = id
		}
	}

	timeoutStr := r.URL.Query().Get("timeout")
	timeout := 30 * time.Second
	if timeoutStr != "" {
		if t, err := strconv.Atoi(timeoutStr); err == nil && t > 0 && t <= 60 {
			timeout = time.Duration(t) * time.Second
		}
	}

	log.Printf("Poll request: since=%d, timeout=%v", sinceID, timeout)

	messages := broker.GetMessagesSince(sinceID, timeout)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"messages": messages,
		"count":    len(messages),
	})
}

func handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Text string `json:"text"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Text == "" {
		http.Error(w, "Text is required", http.StatusBadRequest)
		return
	}

	msg := broker.AddMessage(req.Text)
	log.Printf("New message: id=%d, text=%s", msg.ID, msg.Text)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(msg)
}

func handleMessages(w http.ResponseWriter, r *http.Request) {
	messages := broker.GetAllMessages()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"messages": messages,
		"count":    len(messages),
	})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

const clientHTML = `<!DOCTYPE html>
<html>
<head>
    <title>Long Polling Test Client</title>
    <style>
        body { font-family: monospace; max-width: 900px; margin: 50px auto; padding: 20px; }
        h1 { color: #333; }
        .controls { margin: 20px 0; padding: 20px; border: 1px solid #ddd; border-radius: 8px; }
        label { display: inline-block; width: 100px; }
        input[type="text"] { width: 300px; padding: 5px; }
        input[type="number"] { width: 80px; padding: 5px; }
        button { padding: 10px 20px; margin: 10px 5px 10px 0; cursor: pointer; }
        button:disabled { opacity: 0.5; cursor: not-allowed; }
        .status { padding: 10px; margin: 10px 0; border-radius: 4px; font-size: 14px; }
        .status.polling { background: #fff3cd; border: 1px solid #ffc107; }
        .status.connected { background: #d4edda; border: 1px solid #28a745; }
        .status.disconnected { background: #f8d7da; border: 1px solid #dc3545; }
        .messages { background: #f5f5f5; padding: 15px; margin: 20px 0; border-radius: 4px;
                    max-height: 400px; overflow-y: auto; }
        .message { padding: 8px; margin: 5px 0; background: white; border-radius: 3px;
                   border-left: 3px solid #4caf50; }
        .message .id { color: #666; font-size: 12px; }
        .message .time { color: #999; font-size: 11px; margin-left: 10px; }
        .message .text { margin-top: 5px; }
        #log { background: #1a1a1a; color: #0f0; padding: 15px; height: 150px; overflow-y: auto;
               border-radius: 4px; font-size: 13px; }
        .stats { color: #666; font-size: 14px; margin: 10px 0; }
        .info { color: #888; margin-top: 20px; font-size: 12px; }
    </style>
</head>
<body>
    <h1>Long Polling Test Client</h1>

    <div class="controls">
        <h2>Settings</h2>
        <div>
            <label>Timeout (s):</label>
            <input type="number" id="pollTimeout" value="30" min="1" max="60">
        </div>
        <div style="margin-top: 10px;">
            <button id="startBtn" onclick="startPolling()">Start Polling</button>
            <button id="stopBtn" onclick="stopPolling()" disabled>Stop Polling</button>
        </div>
        <div class="status disconnected" id="status">Not polling</div>
    </div>

    <div class="controls">
        <h2>Send Message</h2>
        <div>
            <input type="text" id="messageText" placeholder="Enter message...">
            <button onclick="sendMessage()">Send</button>
        </div>
    </div>

    <div class="stats" id="stats">Messages: 0 | Polls: 0 | Last poll: never</div>

    <div class="messages" id="messages"></div>

    <div id="log"></div>

    <div class="info">
        <p>Long polling workflow:</p>
        <ul>
            <li>Client sends HTTP request and server holds it open until new data arrives</li>
            <li>When data arrives or timeout expires, server responds</li>
            <li>Client immediately sends a new request (long poll)</li>
            <li>Simulates real-time updates without WebSockets</li>
        </ul>
    </div>

    <script>
        const logEl = document.getElementById('log');
        const messagesEl = document.getElementById('messages');
        const statusEl = document.getElementById('status');
        const statsEl = document.getElementById('stats');

        let polling = false;
        let lastMessageID = 0;
        let pollCount = 0;
        let lastPollTime = null;

        function log(msg, type = 'info') {
            const time = new Date().toLocaleTimeString();
            const colors = { info: '#0f0', error: '#f00', success: '#0ff', warn: '#ff0' };
            logEl.innerHTML += '<div style="color:' + (colors[type] || '#0f0') + '">[' + time + '] ' + msg + '</div>';
            logEl.scrollTop = logEl.scrollHeight;
        }

        function updateStatus(status, className) {
            statusEl.textContent = status;
            statusEl.className = 'status ' + className;
        }

        function updateStats() {
            const lastPoll = lastPollTime ? new Date(lastPollTime).toLocaleTimeString() : 'never';
            statsEl.textContent = 'Messages: ' + lastMessageID + ' | Polls: ' + pollCount + ' | Last poll: ' + lastPoll;
        }

        function displayMessage(msg) {
            const time = new Date(msg.timestamp).toLocaleTimeString();
            const div = document.createElement('div');
            div.className = 'message';
            div.innerHTML = '<div class="id">ID: ' + msg.id + '<span class="time">' + time + '</span></div>' +
                          '<div class="text">' + msg.text + '</div>';
            messagesEl.appendChild(div);
            messagesEl.scrollTop = messagesEl.scrollHeight;
        }

        async function poll() {
            if (!polling) return;

            const timeout = document.getElementById('pollTimeout').value;
            updateStatus('Polling... (timeout: ' + timeout + 's)', 'polling');

            try {
                const startTime = Date.now();
                const response = await fetch('/poll?since=' + lastMessageID + '&timeout=' + timeout);
                const data = await response.json();
                const elapsed = ((Date.now() - startTime) / 1000).toFixed(2);

                pollCount++;
                lastPollTime = Date.now();
                updateStats();

                if (data.messages && data.messages.length > 0) {
                    log('Received ' + data.messages.length + ' message(s) after ' + elapsed + 's', 'success');
                    data.messages.forEach(msg => {
                        displayMessage(msg);
                        lastMessageID = Math.max(lastMessageID, msg.id);
                    });
                } else {
                    log('Poll timeout after ' + elapsed + 's (no new messages)', 'info');
                }

                updateStatus('Connected (last poll: ' + elapsed + 's)', 'connected');
                updateStats();

                if (polling) {
                    setTimeout(poll, 100);
                }
            } catch (e) {
                log('Poll error: ' + e.message, 'error');
                updateStatus('Error: ' + e.message, 'disconnected');

                if (polling) {
                    setTimeout(poll, 2000);
                }
            }
        }

        function startPolling() {
            if (polling) return;
            polling = true;
            document.getElementById('startBtn').disabled = true;
            document.getElementById('stopBtn').disabled = false;
            log('Started polling', 'success');
            poll();
        }

        function stopPolling() {
            if (!polling) return;
            polling = false;
            document.getElementById('startBtn').disabled = false;
            document.getElementById('stopBtn').disabled = true;
            updateStatus('Stopped polling', 'disconnected');
            log('Stopped polling', 'warn');
        }

        async function sendMessage() {
            const text = document.getElementById('messageText').value;
            if (!text) return;

            try {
                const response = await fetch('/send', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ text: text })
                });
                const msg = await response.json();
                log('Sent message: "' + text + '" (id: ' + msg.id + ')', 'success');
                document.getElementById('messageText').value = '';
            } catch (e) {
                log('Send error: ' + e.message, 'error');
            }
        }

        document.getElementById('messageText').addEventListener('keypress', function(e) {
            if (e.key === 'Enter') {
                sendMessage();
            }
        });

        async function loadExistingMessages() {
            try {
                const response = await fetch('/messages');
                const data = await response.json();
                if (data.messages && data.messages.length > 0) {
                    data.messages.forEach(msg => {
                        displayMessage(msg);
                        lastMessageID = Math.max(lastMessageID, msg.id);
                    });
                    updateStats();
                    log('Loaded ' + data.messages.length + ' existing message(s)');
                }
            } catch (e) {
                log('Failed to load messages: ' + e.message, 'error');
            }
        }

        loadExistingMessages();
    </script>
</body>
</html>`

func autoMessageGenerator(broker *MessageBroker) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	messages := []string{
		"System notification: All services operational",
		"Update available: New features deployed",
		"Reminder: Check your notifications",
		"Alert: High activity detected",
		"Info: Database backup completed",
	}

	index := 0
	for range ticker.C {
		msg := messages[index%len(messages)]
		broker.AddMessage(msg)
		log.Printf("Auto-generated message: %s", msg)
		index++
	}
}

func main() {
	addr := flag.String("addr", ":8080", "HTTP service address")
	autoGen := flag.Bool("autogen", true, "Enable auto-message generation")
	flag.Parse()

	broker = NewMessageBroker()

	if *autoGen {
		go autoMessageGenerator(broker)
	}

	http.HandleFunc("/poll", handlePoll)
	http.HandleFunc("/send", handleSend)
	http.HandleFunc("/messages", handleMessages)
	http.HandleFunc("/health", handleHealth)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(clientHTML))
	})

	log.Printf("Starting long-polling server on %s (auto-gen: %v)", *addr, *autoGen)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
