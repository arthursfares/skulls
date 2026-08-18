package main

import (
	"log"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/joho/godotenv"

	"rtc-client/backend"
	"rtc-client/screens"
)

func main() {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		log.Fatalf("Error loading .env file: %v", err)
	}
	m := screens.NewModel()
	p := tea.NewProgram(m)
	backend.SetProgram(p) // package-level hook the networking/audio side sends messages through
	defer backend.StopAudio()
	if _, err := p.Run(); err != nil { log.Fatal(err) }
}
