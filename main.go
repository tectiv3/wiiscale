package main

import (
	"fmt"
	"os"

	"github.com/tectiv3/wiiscale/wiiboard"
)

func main() {
	board, err := wiiboard.Detect()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	battery, err := board.Battery()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	fmt.Printf("Battery level: %d%%\n", battery)
	go board.Listen()

	for weight := range board.Weights {
		fmt.Printf("Got weight: %0.2f\n", weight/100)
	}
}
