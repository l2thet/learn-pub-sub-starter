package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

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

	gs := gamelogic.NewGameState(username)

	go func() {
		if err := pubsub.SubscribeJSON(
			con,
			routing.ExchangePerilDirect,
			fmt.Sprintf("pause.%s", username),
			routing.PauseKey,
			2,
			handlerPause(gs),
		); err != nil {
			log.Fatalf("Failed to set up consumer: %v", err)
		}
	}()

	go func() {
		if err := pubsub.SubscribeJSON(
			con,
			routing.ExchangePerilTopic,
			fmt.Sprintf("army_moves.%s", username),
			"army_moves.*",
			2,
			func(move gamelogic.ArmyMove) pubsub.AckType {
				outcome := gs.HandleMove(move)
				fmt.Print("> ")

				switch outcome {
				case gamelogic.MoveOutComeSafe:
					fallthrough
				case gamelogic.MoveOutcomeMakeWar:
					ch, err := con.Channel()
					if err != nil {
						panic(err)
					}
					defer ch.Close()
					pubsub.PublishJSON(ch, routing.ExchangePerilTopic, fmt.Sprintf("%s.%s", routing.WarRecognitionsPrefix, username), gamelogic.RecognitionOfWar{
						Attacker: move.Player,
						Defender: gs.GetPlayerSnap(),
					})
					log.Printf("NackRequeue move message: %v\n", move)
					return pubsub.Ack
				case gamelogic.MoveOutcomeSamePlayer:
					fallthrough
				default:
					log.Printf("NackDiscard move message: %v\n", move)
					return pubsub.NackDiscard
				}
			},
		); err != nil {
			log.Fatalf("Failed to set up consumer: %v", err)
		}
	}()

	go func() {
		if err := pubsub.SubscribeJSON(
			con,
			routing.ExchangePerilTopic,
			"war",
			fmt.Sprintf("%s.*", routing.WarRecognitionsPrefix),
			1,
			handlerMove(gs, con, username),
		); err != nil {
			log.Fatalf("Failed to set up consumer: %v", err)
		}
	}()

	for {
		words := gamelogic.GetInput()
		if len(words) == 0 {
			continue
		}
		words = strings.Split(strings.ToLower(strings.Join(words, " ")), " ")
		cmd := words[0]
		switch cmd {
		case "spawn":
			err = gs.CommandSpawn(words)
			if err != nil {
				fmt.Println("error: " + err.Error())
			}
			fmt.Printf("Spawning at location: %s with unit: %s \n", words[1], words[2])
		case "move":
			_, err = gs.CommandMove(words)
			if err != nil {
				fmt.Println(err)
			}

			ch, err := con.Channel()
			if err != nil {
				panic(err)
			}
			defer ch.Close()

			var unitsToMove []gamelogic.Unit
			playerUnits := gs.GetPlayerSnap().Units
			for _, idStr := range words[2:] {
				id, err := strconv.Atoi(idStr) // Convert string to int
				if err != nil {
					// Handle error (maybe log it)
					fmt.Printf("Invalid unit ID: %s\n", idStr)
					continue
				}

				if id >= 0 && id < len(playerUnits) {
					unitsToMove = append(unitsToMove, playerUnits[id])
				} else {
					fmt.Printf("Unit ID out of range: %d\n", id)
				}
			}

			pubsub.PublishJSON(ch, routing.ExchangePerilTopic, "army_moves."+username, gamelogic.ArmyMove{
				Player:     gs.GetPlayerSnap(),
				Units:      unitsToMove,
				ToLocation: gamelogic.Location(words[1]),
			})
			fmt.Printf("Published move unit: %s to location: %s \n", words[2], words[1])
		case "status":
			gs.CommandStatus()
		case "help":
			gamelogic.PrintClientHelp()
		case "spam":
			ch, err := con.Channel()
			if err != nil {
				panic(err)
			}
			defer ch.Close()

			loopCount, err := strconv.Atoi(words[1])
			if err != nil {
				fmt.Println("error: " + err.Error())
				panic(err)
			}
			for i := 0; i < loopCount; i++ {
				msg := gamelogic.GetMaliciousLog()
				mlog := routing.GameLog{
					CurrentTime: time.Now(),
					Message:     msg,
					Username:    username,
				}
				pubsub.PublishGob(ch, routing.ExchangePerilTopic, fmt.Sprintf("%s.%s", routing.GameLogSlug, username), mlog)
			}
			fmt.Printf("Spamming %d moves \n", loopCount)
		case "quit":
			gamelogic.PrintQuit()
			return
		default:
			fmt.Println("Unknown command. Type 'help' for a list of commands.")
		}

	}

	// wait for ctrl+c
	// signalChan := make(chan os.Signal, 1)
	// signal.Notify(signalChan, os.Interrupt)
	// <-signalChan
}

func handlerPause(gs *gamelogic.GameState) func(routing.PlayingState) pubsub.AckType {
	return func(ps routing.PlayingState) pubsub.AckType {
		defer fmt.Print("> ")
		gs.HandlePause(ps)
		log.Printf("Acking pause message: %v\n", ps)
		return pubsub.Ack
	}
}

func handlerMove(gs *gamelogic.GameState, con *amqp.Connection, username string) func(gamelogic.RecognitionOfWar) pubsub.AckType {
	return func(row gamelogic.RecognitionOfWar) pubsub.AckType {
		defer fmt.Print("> ")

		outcome, winner, loser := gs.HandleWar(row)

		switch outcome {
		case gamelogic.WarOutcomeNotInvolved:
			return pubsub.NackRequeue
		case gamelogic.WarOutcomeNoUnits:
			return pubsub.Ack
		}

		ch, err := con.Channel()
		if err != nil {
			fmt.Printf("Failed to get channel: %v\n", err)
			return pubsub.NackRequeue
		}
		defer ch.Close()

		switch outcome {
		case gamelogic.WarOutcomeOpponentWon:
			msg := fmt.Sprintf("%s won a war against %s", winner, loser)
			err = pubsub.PublishGob(ch, routing.ExchangePerilTopic, fmt.Sprintf("%s.%s", routing.GameLogSlug, username), routing.GameLog{
				CurrentTime: time.Now(),
				Message:     msg,
				Username:    username,
			})
			if err != nil {
				fmt.Printf("Failed to publish gob message: %v\n", err)
				return pubsub.NackRequeue
			}
			return pubsub.Ack
		case gamelogic.WarOutcomeYouWon:
			msg := fmt.Sprintf("%s won a war against %s", winner, loser)
			err = pubsub.PublishGob(ch, routing.ExchangePerilTopic, fmt.Sprintf("%s.%s", routing.GameLogSlug, username), routing.GameLog{
				CurrentTime: time.Now(),
				Message:     msg,
				Username:    username,
			})
			if err != nil {
				fmt.Printf("Failed to publish gob message: %v\n", err)
				return pubsub.NackRequeue
			}
			return pubsub.Ack
		case gamelogic.WarOutcomeDraw:
			msg := fmt.Sprintf("A war between %s and %s resulted in a draw", winner, loser)
			err = pubsub.PublishGob(ch, routing.ExchangePerilTopic, fmt.Sprintf("%s.%s", routing.GameLogSlug, username), routing.GameLog{
				CurrentTime: time.Now(),
				Message:     msg,
				Username:    username,
			})
			if err != nil {
				fmt.Printf("Failed to publish gob message: %v\n", err)
				return pubsub.NackRequeue
			}
			return pubsub.Ack
		default:
			fmt.Printf("Unknown war outcome: %v\n", outcome)
			return pubsub.NackRequeue
		}
	}
}
