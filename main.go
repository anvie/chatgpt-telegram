package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/m1guelpf/chatgpt-telegram/src/chatgpt"
	"github.com/m1guelpf/chatgpt-telegram/src/config"
	"github.com/m1guelpf/chatgpt-telegram/src/session"
	"github.com/m1guelpf/chatgpt-telegram/src/tgbot"
)

type ChatGPTAgents struct {
	Agents []*chatgpt.ChatGPT
}


func main() {

	agents := ChatGPTAgents{}
	agentLoadedCount := int64(0)

	for i := 1; i <= 3; i++ {
		chatgptConfig := fmt.Sprintf("chatgpt%d", i)
		persistentConfig, err := config.LoadOrCreatePersistentConfig(chatgptConfig)
		if err != nil {
			log.Println("Couldn't load config: %v", err)
			break;
		}

		if persistentConfig.OpenAISession == "" {
			token, err := session.GetSession()
			if err != nil {
				log.Fatalf("Couldn't get OpenAI session: %v", err)
			}

			if err = persistentConfig.SetSessionToken(token); err != nil {
				log.Fatalf("Couldn't save OpenAI session: %v", err)
			}
		}

		chatGPT := chatgpt.Init(persistentConfig)
		agents.Agents = append(agents.Agents, chatGPT)
		agentLoadedCount++
		log.Printf("Started ChatGPT %d!", i)
	}

	envConfig, err := config.LoadEnvConfig(".env")
	if err != nil {
		log.Fatalf("Couldn't load .env config: %v", err)
	}
	if err := envConfig.ValidateWithDefaults(); err != nil {
		log.Fatalf("Invalid .env config: %v", err)
	}

	bot, err := tgbot.New(envConfig.TelegramToken, time.Duration(envConfig.EditWaitSeconds))
	if err != nil {
		log.Fatalf("Couldn't start Telegram bot: %v", err)
	}

	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		bot.Stop()
		os.Exit(0)
	}()

	log.Printf("Started Telegram bot! Message @%s to start.", bot.Username)

	for update := range bot.GetUpdatesChan() {
		if update.Message == nil {
			continue
		}

		var (
			updateText      = update.Message.Text
			updateChatID    = update.Message.Chat.ID
			updateMessageID = update.Message.MessageID
			updateUserID    = update.Message.From.ID
		)

		if len(envConfig.TelegramID) != 0 && !envConfig.HasTelegramID(updateUserID) {
			log.Printf("User %d is not allowed to use this bot", updateUserID)
			bot.Send(updateChatID, updateMessageID, "You are not authorized to use this bot.")
			continue
		}

		agentIndex := updateUserID % agentLoadedCount

		fmt.Printf("incoming chat, handled using agent index: %d\n", agentIndex)

		if !update.Message.IsCommand() {
			bot.SendTyping(updateChatID)

			chatGPT := agents.Agents[agentIndex]

			feed, err := chatGPT.SendMessage(updateText, updateChatID)
			if err != nil {
				bot.Send(updateChatID, updateMessageID, fmt.Sprintf("Error: %v", err))
			} else {
				bot.SendAsLiveOutput(updateChatID, updateMessageID, feed)
			}
			continue
		}

		var text string
		switch update.Message.Command() {
		case "help":
			text = "Send a message to start talking with ChatGPT. You can use /reload at any point to clear the conversation history and start from scratch (don't worry, it won't delete the Telegram messages)."
		case "start":
			text = "Send a message to start talking with ChatGPT. You can use /reload at any point to clear the conversation history and start from scratch (don't worry, it won't delete the Telegram messages)."
		case "reload":
			chatGPT := agents.Agents[agentIndex]
			chatGPT.ResetConversation(updateChatID)
			text = "Started a new conversation. Enjoy!"
		default:
			text = "Unknown command. Send /help to see a list of commands."
		}

		if _, err := bot.Send(updateChatID, updateMessageID, text); err != nil {
			log.Printf("Error sending message: %v", err)
		}
	}
}
