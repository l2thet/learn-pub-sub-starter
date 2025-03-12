package main

import (
	"fmt"
	"os"
	"os/signal"

	"github.com/bootdotdev/learn-pub-sub-starter/internal/gamelogic"
	"github.com/bootdotdev/learn-pub-sub-starter/internal/pubsub"
	"github.com/bootdotdev/learn-pub-sub-starter/internal/routing"
	amqp "github.com/rabbitmq/amqp091-go"
)

func main() {
	fmt.Println("Starting Peril client...")
	envCon := "amqp://guest:guest@localhost:5672/"
	con, err := amqp.Dial(envCon)
	if err != nil {
		panic(err)
	}
	defer con.Close()

	username, err := gamelogic.ClientWelcome()
	if err != nil {
		panic(err)
	}

	pubsub.DeclareAndBind(con, routing.ExchangePerilDirect, routing.PauseKey+"."+username, routing.PauseKey, 2)

	// wait for ctrl+c
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt)
	<-signalChan
}
