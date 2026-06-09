package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	_ "github.com/lib/pq"
)

// ─── Models ──────────────────────────────────────────────────────────────────

type Book struct {
	ID        int       `json:"id"`
	Title     string    `json:"title"`
	Author    string    `json:"author"`
	CreatedAt time.Time `json:"created_at"`
}

// ─── In-memory fallback ───────────────────────────────────────────────────────

type memStore struct {
	mu      sync.Mutex
	books   []Book
	counter int
}

func (s *memStore) list() []Book {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Book, len(s.books))
	copy(out, s.books)
	return out
}

func (s *memStore) add(title, author string) Book {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counter++
	b := Book{ID: s.counter, Title: title, Author: author, CreatedAt: time.Now().UTC()}
	s.books = append(s.books, b)
	return b
}

// ─── App ─────────────────────────────────────────────────────────────────────

type app struct {
	db    *sql.DB
	store *memStore
}

func newApp() *app {
	a := &app{}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL != "" {
		db, err := sql.Open("postgres", dbURL)
		if err == nil && db.Ping() == nil {
			log.Println("✅ Connected to PostgreSQL")
			a.db = db
			a.migrate()
			return a
		}
		log.Println("⚠️  PostgreSQL unreachable — falling back to memory store")
		if db != nil {
			db.Close()
		}
	} else {
		log.Println("ℹ️  DATABASE_URL not set — using in-memory store")
	}
	a.store = &memStore{
		books:   []Book{{ID: 1, Title: "The Go Programming Language", Author: "Alan Donovan", CreatedAt: time.Now().UTC()}},
		counter: 1,
	}
	return a
}

func (a *app) migrate() {
	_, err := a.db.Exec(`CREATE TABLE IF NOT EXISTS books (
		id         SERIAL PRIMARY KEY,
		title      TEXT NOT NULL,
		author     TEXT NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`)
	if err != nil {
		log.Printf("⚠️  migrate: %v", err)
	}
}

// ─── Handlers ─────────────────────────────────────────────────────────────────

func (a *app) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *app) getBooks(w http.ResponseWriter, r *http.Request) {
	if a.db != nil {
		rows, err := a.db.Query(`SELECT id, title, author, created_at FROM books ORDER BY id`)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer rows.Close()
		books := []Book{}
		for rows.Next() {
			var b Book
			if err := rows.Scan(&b.ID, &b.Title, &b.Author, &b.CreatedAt); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			books = append(books, b)
		}
		writeJSON(w, http.StatusOK, books)
		return
	}
	writeJSON(w, http.StatusOK, a.store.list())
}

func (a *app) postBooks(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Title  string `json:"title"`
		Author string `json:"author"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if input.Title == "" || input.Author == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title and author are required"})
		return
	}

	if a.db != nil {
		var b Book
		err := a.db.QueryRow(
			`INSERT INTO books (title, author) VALUES ($1, $2) RETURNING id, title, author, created_at`,
			input.Title, input.Author,
		).Scan(&b.ID, &b.Title, &b.Author, &b.CreatedAt)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, b)
		return
	}
	writeJSON(w, http.StatusCreated, a.store.add(input.Title, input.Author))
}

// ─── Router ───────────────────────────────────────────────────────────────────

func (a *app) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		a.health(w, r)
	})
	mux.HandleFunc("/books", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			a.getBooks(w, r)
		case http.MethodPost:
			a.postBooks(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	return mux
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	a := newApp()
	addr := fmt.Sprintf("0.0.0.0:%s", port)
	log.Printf("🚀 fooshika-backend listening on %s", addr)
	srv := &http.Server{
		Addr:         addr,
		Handler:      a.routes(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}