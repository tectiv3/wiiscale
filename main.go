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
	}
	fmt.Println(battery)
	go board.Listen()
	// board.Calibrate()
	for weight := range board.Weights {
		// fmt.Printf("%+v. Total: %0.2f, calibrated: %0.2f\n", event, event.Total/100, board.GetCalibrated()/100)
		fmt.Printf("Got weight: %0.2f\n", weight)
	}
}
