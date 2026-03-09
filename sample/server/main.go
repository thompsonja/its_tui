package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

func main() {
	env := os.Getenv("APP_ENV")

	mux := http.NewServeMux()
	mux.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		message := "hello world"
		if env == "test" {
			message = "hello universe"
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if err := json.NewEncoder(w).Encode(map[string]string{"message": message}); err != nil {
			http.Error(w, "encoding error", http.StatusInternalServerError)
		}
	})

	port := "8080"
	fmt.Printf("server starting on :%s (APP_ENV=%q)\n", port, env)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
