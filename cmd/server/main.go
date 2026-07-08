package main

import (
	"log"
	"net/http"
	"strconv"

	"img2color/config"
	"img2color/internal/bootstrap"
)

func main() {
	cfg := config.Load()
	addr := ":" + strconv.Itoa(cfg.Port)
	log.Printf("img2color listening on %s", addr)
	if err := http.ListenAndServe(addr, bootstrap.NewService().Router()); err != nil {
		log.Fatal(err)
	}
}
