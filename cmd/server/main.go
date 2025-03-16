package main

import (
	"fmt"
	"strings"

	"github.com/bootdotdev/learn-pub-sub-starter/internal/gamelogic"
	"github.com/bootdotdev/learn-pub-sub-starter/internal/pubsub"
	"github.com/bootdotdev/learn-pub-sub-starter/internal/routing"
	amqp "github.com/rabbitmq/amqp091-go"
)

func main() {
	fmt.Println("Starting Peril server...")
	envCon := "amqp://guest:guest@localhost:5672/"
	con, err := amqp.Dial(envCon)
	if err != nil {
		panic(err)
	}
	defer con.Close()

	newChan, err := con.Channel()
	if err != nil {
		panic(err)
	}
	defer newChan.Close()

	// pubsub.PublishJSON(newChan, routing.ExchangePerilDirect, routing.PauseKey, routing.PlayingState{
	// 	IsPaused: true,
	// })
	pubsub.DeclareAndBind(con, routing.ExchangePerilTopic, routing.GameLogSlug, "game_logs.*", 1)

	fmt.Println("Connected to RabbitMQ")

	for {
		words := gamelogic.GetInput()
		if len(words) == 0 {
			continue
		}
		cmd := strings.ToLower(words[0])
		switch cmd {
		case "pause":
			fmt.Println("Pausing game...")
			pubsub.PublishJSON(newChan, routing.ExchangePerilDirect, routing.PauseKey, routing.PlayingState{
				IsPaused: true,
			})
		case "resume":
			fmt.Println("Resuming game...")
			pubsub.PublishJSON(newChan, routing.ExchangePerilDirect, routing.PauseKey, routing.PlayingState{
				IsPaused: false,
			})
		case "quit":
			fmt.Println("Shutting down...")
			return
		case "help":
			gamelogic.PrintServerHelp()
		default:
			fmt.Println("Unknown command. Type 'help' for a list of commands.")
		}

		// wait for ctrl+c
		// signalChan := make(chan os.Signal, 1)
		// signal.Notify(signalChan, os.Interrupt)
		// <-signalChan

	}

}
