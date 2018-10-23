package main

import (
	"log"

	"github.com/tectiv3/wiiscale/wiiboard"
)

func main() {
	board := wiiboard.New()
	if err := board.Detect(); err != nil {
		log.Println(err)
		return
	}

	if battery, err := board.Battery(); err != nil {
		log.Println(err)
		return
	} else {
		log.Printf("Battery level: %d%%\n", battery)
	}

	go board.Listen()

	for weight := range board.Weights {
		log.Printf("Got weight: %0.2f\n", weight/100)
	}
}
