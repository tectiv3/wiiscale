package main

import (
	"fmt"

	"github.com/tectiv3/wiiscale/wiiboard"
)

func main() {
	board := wiiboard.New()
	if err := board.Detect(); err != nil {
		fmt.Println(err)
		return
	}

	if battery, err := board.Battery(); err != nil {
		fmt.Println(err)
		return
	} else {
		fmt.Printf("Battery level: %d%%\n", battery)
	}

	go board.Listen()

	for weight := range board.Weights {
		fmt.Printf("Got weight: %0.2f\n", weight/100)
	}
}
