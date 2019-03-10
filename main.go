package main

import (
	"fmt"
	"log"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/tectiv3/wiiscale/wiiboard"
)

var f mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
	log.Printf("TOPIC: %s\n", msg.Topic())
	log.Printf("MSG: %s\n", msg.Payload())
}

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

	// mqtt.DEBUG = log.New(os.Stdout, "", 0)
	// mqtt.ERROR = log.New(os.Stdout, "", 0)
	opts := mqtt.NewClientOptions().AddBroker("tcp://192.168.100.111:1883").SetClientID("wiiboard")
	opts.SetKeepAlive(2 * time.Second)
	opts.SetDefaultPublishHandler(f)
	opts.SetPingTimeout(1 * time.Second)

	c := mqtt.NewClient(opts)
	if token := c.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	log.Println("Connected to mqtt")

	go func() {
		for weight := range board.Weights {
			w := weight / 100
			w += 1.0
			token := c.Publish("sensors/wiiboard/raw", 0, false, fmt.Sprintf(`{"weight": %0.2f}`, w))
			token.Wait()
		}
	}()

	for weight := range board.Weight {
		w := weight / 100
		name := "kin"
		if w < 60.0 {
			name = "santy"
		} else if w > 81.0 {
			name = "domi"
		}
		w += 1.7
		log.Printf("%s: %0.2f\n", name, w)

		// msg, _ := json.Marshal(struct {
		//     Weight float64 `json:"weight"`
		//     Person string  `json:"name"`
		// }{w, name})
		// token := c.Publish("sensors/wiiboard/last", 0, false, msg)

		token := c.Publish("sensors/wiiboard/last", 0, false, fmt.Sprintf(`{"%s": %0.2f}`, name, w))
		token.Wait()
	}
}
