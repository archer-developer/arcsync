package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"arcsync/internal/realtime"
	"arcsync/internal/room"
	"arcsync/internal/syncclock"
	arcsyncweb "arcsync/web"

	"github.com/gorilla/websocket"
)

var roomIDRe = regexp.MustCompile(`^[A-Z0-9]{6}$`)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(*http.Request) bool { return true },
}

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	flag.Parse()

	registry := room.NewRegistry()
	var hubsMu sync.RWMutex
	hubs := make(map[string]*realtime.Hub)
	clk := syncclock.Real()

	fileServer := http.FileServer(http.FS(arcsyncweb.FS))

	mux := http.NewServeMux()

	// Serve index.html for all /room/* paths (SPA routing)
	indexHTML, err := arcsyncweb.FS.ReadFile("index.html")
	if err != nil {
		log.Fatal("could not read embedded index.html")
	}
	mux.HandleFunc("/room/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexHTML)
	})

	// Create a room
	mux.HandleFunc("/api/rooms", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		rm, err := registry.Create()
		if err != nil {
			log.Printf("create room: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		hub := realtime.NewHub(rm, clk)
		hubsMu.Lock()
		hubs[rm.ID] = hub
		hubsMu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"room_id":    rm.ID,
			"host_token": rm.HostToken,
		})
	})

	// WebSocket endpoint
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		rawID := strings.TrimSpace(r.URL.Query().Get("room"))
		roomID := strings.ToUpper(rawID)
		if !roomIDRe.MatchString(roomID) {
			http.Error(w, "invalid room ID", http.StatusBadRequest)
			return
		}
		hubsMu.RLock()
		hub, ok := hubs[roomID]
		hubsMu.RUnlock()
		if !ok {
			http.Error(w, "room not found", http.StatusNotFound)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("ws upgrade: %v", err)
			return
		}
		c := realtime.NewClient(hub, conn, roomID)
		hub.Register(c)
		go c.WritePump()
		go c.ReadPump()
	})

	// Static files
	mux.Handle("/", fileServer)

	srv := &http.Server{
		Addr:         *addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	log.Printf("Sync Start listening on %s", *addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
